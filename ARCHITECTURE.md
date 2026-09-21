# Decisões de Arquitetura e Engenharia de Sistemas — Ledger Wagering Service

Este documento detalha as decisões técnicas, escolhas de design e abordagens de resiliência adotadas para resolver o desafio de processamento distribuído de transações e ledger financeiro.

---

## 1. Precisão Monetária e Design do Tipo `Money`

* **Abordagem:** Proibição do uso de tipos de ponto flutuante (`float32` ou `float64`) para valores financeiros, evitando erros de arredondamento e representação binária.

* **Implementação:** O objeto de domínio `Money` é modelado como um **Value Object imutável**, utilizando escala fixa baseada em inteiros de 64 bits (`int64`). O valor representa as unidades mínimas da moeda. Por exemplo, `25.00` é representado internamente como `2500`.

* **Parsing:** Valores monetários são recebidos como strings decimais e validados de acordo com a escala suportada. Valores com escala inválida ou formato não suportado são rejeitados.

* **Blindagem:** As operações `Add`, `Subtract` e `Negate` validam limites numéricos e impedem overflow de `int64`.

* **Persistência:** O banco armazena valores monetários utilizando representação inteira, mantendo a precisão durante todo o ciclo financeiro.

---

## 2. Coordenação de Concorrência Distribuída e Lost Updates

### Desafio

Garantir consistência financeira com múltiplas instâncias da aplicação executando simultaneamente e disputando o saldo da mesma carteira.

Exemplo:

> Saldo de R$ 100,00 recebendo duas apostas concorrentes de R$ 80,00.

### Solução

Foi adotado **Pessimistic Locking** diretamente no PostgreSQL através de:

```sql
SELECT ... FOR UPDATE
```

complementado por controle de versão através de:

```sql
UPDATE ... WHERE version = $expected
```

### Justificativa

Locks mantidos exclusivamente na memória da aplicação, como `sync.Mutex`, não seriam suficientes em um ambiente distribuído.

O lock no PostgreSQL garante que operações concorrentes sobre a **mesma carteira** sejam serializadas.

Assim:

1. a primeira transação bloqueia a carteira;
2. lê o saldo atual;
3. valida os fundos;
4. atualiza saldo e versão;
5. registra o movimento no ledger;
6. realiza o commit;
7. a segunda transação obtém o lock;
8. lê o novo saldo;
9. é rejeitada caso não existam fundos suficientes.

Carteiras diferentes não compartilham um lock global e podem ser processadas em paralelo.

---

## 3. Idempotência Persistente e Integridade do Ledger

### Deduplicação

O banco impõe unicidade para identificadores financeiros relevantes, incluindo:

```text
(provider_id, external_transaction_id)
```

e `idempotency_key`.

As verificações de idempotência e as alterações financeiras ocorrem dentro da mesma transação PostgreSQL.

### Hash de Payload

Quando uma chave de idempotência é utilizada novamente, o sistema compara o payload recebido com o payload originalmente processado através de hash SHA-256.

O mesmo identificador com conteúdo diferente é tratado como conflito e resulta em:

```text
HTTP 409 Conflict
```

Isso impede que uma mesma chave represente duas operações financeiras diferentes.

### Ledger Append-Only

O histórico financeiro é tratado como imutável.

A tabela `wallet_ledger_entries` possui proteção no banco contra alterações posteriores, incluindo bloqueio de `UPDATE` e `DELETE`.

O ledger registra o resultado financeiro de cada operação e permite reconstruir o saldo.

Constraints e regras de domínio impedem saldo negativo.

---

## 4. Mensageria Resiliente — Inbox e Transactional Outbox

### Inbox

O consumidor SQS utiliza um Inbox persistente.

O `message_id` é registrado em `inbox_messages` na mesma transação que realiza:

* processamento da transação;
* alteração da wallet;
* lançamento no ledger;
* criação dos eventos de outbox.

O fluxo principal é realizado através de:

```text
ProcessTransactionWithInbox
```

Caso a mesma mensagem seja recebida novamente, a restrição de unicidade permite identificar o processamento anterior e evitar um novo movimento financeiro.

Isso fornece **exactly-once lógico para o efeito financeiro**, apesar de o transporte SQS possuir semântica de entrega at-least-once.

### Transactional Outbox

Eventos de integração são persistidos em `outbox_events` na mesma transação que realiza a operação financeira.

O evento só é publicado posteriormente por um worker dedicado.

Portanto:

```text
Financial Transaction
        |
        +--> Wallet
        +--> Ledger
        +--> Inbox
        +--> Outbox
                 |
                 | commit
                 v
          Outbox Publisher
                 |
                 v
          EVENTS_QUEUE_URL
```

A publicação não ocorre antes do commit financeiro.

A fila de eventos é separada da fila de ingestão de transações.

### Concorrência no Outbox

Publishers concorrentes utilizam:

```sql
SELECT ... FOR UPDATE SKIP LOCKED
```

para evitar que dois workers processem simultaneamente o mesmo evento.

Falhas de publicação são persistidas e podem ser reprocessadas posteriormente, com backoff e limite de tentativas.

### REFUND / ROLLBACK

Operações que dependem de uma transação de referência podem chegar antes da operação original.

Nesse caso, o sistema mantém o estado:

```text
PENDING_REFERENCE
```

e persiste:

```text
next_retry_at
```

Um worker dedicado tenta resolver a referência posteriormente.

Como o estado é persistido no PostgreSQL, o processamento pode continuar após um restart da aplicação.

---

## 5. Autenticação e Isolamento de Provedores via OIDC / Keycloak

### Modelo

A aplicação utiliza Keycloak como Identity Provider OIDC.

O fluxo utilizado para os testes B2B é:

```text
OAuth 2.0 Client Credentials
```

### Validação

O `AuthMiddleware` valida:

* assinatura do JWT;
* JWKS;
* issuer;
* identificação do client/provedor.

As configurações são fornecidas através de:

```text
IDP_ISSUER_URL
IDP_JWKS_URL
```

### Isolamento

A identidade autenticada determina o `providerId` associado à requisição.

Uma operação não pode ser executada em nome de outro provedor.

Tentativas de acessar recursos fora do contexto autorizado são rejeitadas.

---

## 6. HTTP e SQS Compartilham o Mesmo Caso de Uso

As operações financeiras não possuem uma implementação separada para HTTP e SQS.

Ambos os transportes convergem para o mesmo domínio/caso de uso de processamento financeiro.

Isso evita que regras como:

* validação de saldo;
* idempotência;
* ledger;
* transições de estado;
* referências;
* reversões;

tenham comportamentos diferentes dependendo do canal de entrada.

A arquitetura é aproximadamente:

```text
             HTTP
              |
              v
        Auth Middleware
              |
              v
        Wager Use Case
              ^
              |
              |
             SQS
              |
              v
         SQS Worker

                 |
                 v
        PostgreSQL Transaction
        /       |       |      \
    Wallet    Ledger   Inbox   Outbox
```

---

## 7. Persistência e Transações

O PostgreSQL é a fonte persistente de verdade para:

* wallets;
* transações;
* ledger;
* inbox;
* outbox;
* estados pendentes;
* controle de versão.

As operações financeiras críticas são executadas dentro de transações SQL.

A unidade transacional busca manter juntos:

```text
idempotência
+
wallet
+
ledger
+
inbox
+
outbox
```

Isso reduz o risco de estados parciais após falhas.

---

## 8. Recuperação e Redelivery

O sistema foi projetado considerando que mensagens SQS podem ser entregues mais de uma vez.

Uma mensagem não é removida da fila antes de seu processamento financeiro ser concluído.

Fluxo:

```text
ReceiveMessage
      |
      v
ProcessTransactionWithInbox
      |
      +---- erro ----> mensagem permanece disponível
      |
      +---- sucesso --> DeleteMessage
```

Se houver falha após o commit financeiro, mas antes da remoção da mensagem, a mensagem pode ser entregue novamente.

O Inbox identifica o processamento anterior e impede um segundo efeito financeiro.

Payloads inválidos não são silenciosamente processados. O worker registra o erro e permite que a mensagem siga o comportamento de redelivery/DLQ configurado para a fila.

---

## 9. Observabilidade e Ciclo de Vida

### Health Checks

A aplicação disponibiliza:

```text
/health/live
/health/ready
```

O readiness verifica a disponibilidade do PostgreSQL.

### Métricas

A aplicação disponibiliza:

```text
/metrics
```

com métricas relacionadas a:

* wagers processados;
* erros SQS;
* processamento de outbox;
* falhas de processamento.

### Uber Fx

O Uber Fx coordena o lifecycle dos componentes da aplicação.

São gerenciados pelo lifecycle:

* HTTP server;
* SQS worker;
* Outbox worker;
* Pending Reference worker;
* PostgreSQL pool.

O shutdown tenta encerrar os workers de forma coordenada.

---

## 10. Testes de Integração

Os testes de integração estão em:

```text
internal/integration
```

e utilizam a build tag:

```text
integration
```

com ativação através de:

```text
INTEGRATION=1
```

Os testes utilizam infraestrutura real para os componentes principais, incluindo PostgreSQL, Keycloak e LocalStack.

### Concorrência financeira

Duas apostas simultâneas de R$ 80,00 em uma carteira com R$ 100,00 devem produzir:

```text
1 débito efetivo
saldo final: R$ 20,00
```

### Idempotência concorrente

50 goroutines reenviam a mesma transação simultaneamente.

O resultado esperado é:

```text
1 operação financeira
```

sem duplicação do ledger.

### Multi-instância lógica

São utilizados três graphs/pools/use cases independentes apontando para o mesmo PostgreSQL para simular múltiplas instâncias concorrentes.

### Outbox

Dois publishers disputam eventos utilizando `FOR UPDATE SKIP LOCKED`.

O objetivo é garantir que o mesmo evento não seja processado simultaneamente por dois publishers.

### SQS

Os testes cobrem:

* redelivery após commit;
* Inbox;
* ausência de movimento financeiro duplicado;
* envio e processamento utilizando LocalStack FIFO.

### Restart

O estado persistido deve sobreviver ao restart da aplicação, incluindo:

* idempotência;
* transações pendentes;
* outbox;
* referências pendentes.

### E2E

O fluxo E2E utiliza:

```text
Keycloak
   |
   v
JWT / HTTP
   |
   v
Go Application
   |
   +---- PostgreSQL
   |
   +---- LocalStack
```

O teste utiliza autenticação real via Keycloak e infraestrutura persistente.

---

## 11. Validações Manuais Realizadas

Além dos testes automatizados, o fluxo principal foi validado manualmente utilizando a infraestrutura local.

### BET

Foi criada uma wallet com:

```text
100.00 BRL
```

Uma operação:

```text
BET = 80.00 BRL
```

resultou em:

```text
20.00 BRL
```

### Replay

A mesma operação foi reenviada utilizando a mesma chave de idempotência.

O sistema retornou a operação já processada sem realizar novo débito.

### Conflito

A mesma chave de idempotência foi reutilizada com um valor diferente.

Resultado:

```text
HTTP 409 Conflict
```

Nenhum segundo débito foi realizado.

### Reconciliação

O saldo armazenado foi comparado com o saldo calculado a partir do ledger.

Resultado:

```text
consistent = true
```

### WIN via SQS

Uma mensagem `WIN` de:

```text
50.00 BRL
```

foi enviada ao LocalStack.

A aplicação consumiu a mensagem e atualizou o saldo de:

```text
20.00 BRL
```

para:

```text
70.00 BRL
```

Esse fluxo validou:

```text
LocalStack
   |
   v
SQS Worker
   |
   v
Wager Use Case
   |
   v
PostgreSQL
   |
   v
Wallet + Ledger
```

Durante o teste também foi identificado um problema de escaping ao enviar JSON diretamente através do PowerShell/Docker. O payload chegava ao worker sem aspas nas propriedades JSON.

O problema foi resolvido utilizando um arquivo JSON como entrada do AWS CLI dentro do container LocalStack.

---

## 12. Resultados das Verificações Locais

Foram executados com sucesso:

```bash
gofmt -w .
go test ./...
go test -race ./...
go vet ./...
docker compose config
```

A validação manual também confirmou:

* autenticação via Keycloak;
* criação de wallet;
* BET;
* replay idempotente;
* conflito de payload;
* reconciliação;
* processamento de WIN via SQS;
* atualização correta do saldo.

---

## 13. Limitações Conhecidas

### Múltiplas instâncias

Os testes de três instâncias são realizados no mesmo host através de pools/use cases independentes apontando para a mesma base PostgreSQL.

Não é utilizado:

```bash
docker compose --scale
```

para esse cenário de teste.

### Ambiente AWS

Os testes locais utilizam LocalStack para simular SQS.

O ambiente de produção esperado pode utilizar AWS SQS real através da mesma abstração do cliente AWS.

### Identity Provider

O ambiente local utiliza Keycloak provisionado pelo Docker Compose.

---

## 14. Princípios Arquiteturais

As principais decisões podem ser resumidas em:

```text
Dinheiro
  -> inteiros / Money Value Object

Concorrência
  -> PostgreSQL locking + versionamento

Idempotência
  -> banco + Inbox + idempotency key

Histórico financeiro
  -> Ledger Append-Only

Mensageria
  -> SQS FIFO + Inbox

Eventos
  -> Transactional Outbox

Publicação
  -> após commit

Referências ausentes
  -> estado persistente PENDING_REFERENCE

Autenticação
  -> OIDC / Keycloak

Autorização
  -> isolamento por providerId

Lifecycle
  -> Uber Fx

Persistência
  -> PostgreSQL
```

A arquitetura prioriza consistência financeira, idempotência persistente e recuperação após falhas, mantendo os efeitos financeiros dependentes de estado durável em PostgreSQL.
