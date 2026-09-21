# Jungle Gaming — Distributed Wagering Ledger Service

Este é um microserviço financeiro B2B de alta confiabilidade e resiliência transacional, desenvolvido em **Go** para processamento distribuído de operações financeiras de provedores de jogos (iGaming).

O serviço foi projetado para garantir:

* precisão monetária sem uso de `float`;
* idempotência persistente;
* integridade de ledger append-only;
* isolamento entre provedores;
* processamento síncrono via HTTP;
* processamento assíncrono via AWS SQS;
* recuperação segura em cenários de redelivery e falhas;
* consistência sob concorrência;
* autenticação e autorização via OIDC/Keycloak;
* publicação de eventos através de Outbox transacional.

---

## 1. Stack

* Go 1.22+
* Go Modules
* Uber Fx
* `net/http`
* PostgreSQL
* AWS SQS
* LocalStack
* Keycloak / OIDC
* Docker
* Docker Compose

---

## 2. Pré-requisitos

Certifique-se de possuir:

* **Go** 1.22 ou superior
* **Docker**
* **Docker Compose**
* um cliente HTTP, como `curl`, Postman ou Insomnia
* PowerShell no Windows ou shell compatível no Linux/macOS

Verifique:

```bash
go version
docker --version
docker compose version
```

---

## 3. Variáveis de Ambiente

Crie um arquivo `.env` na raiz do projeto.

Recomenda-se copiar o `.env.example`:

```bash
cp .env.example .env
```

No PowerShell:

```powershell
Copy-Item .env.example .env
```

Exemplo:

```env
# Regras de negócio
SUPPORTED_CURRENCIES=BRL,USD,EUR
INITIAL_WALLET_VERSION=1

# PostgreSQL
DB_HOST=localhost
DB_PORT=5432
DB_USER=jungle_user
DB_PASSWORD=jungle_password
DB_NAME=jungle_wagering_ledger
DB_SSLMODE=disable

# AWS / LocalStack
AWS_ACCESS_KEY_ID=test
AWS_SECRET_ACCESS_KEY=test
AWS_REGION=us-east-1
SQS_ENDPOINT=http://localhost:4566

WAGER_QUEUE_URL=http://localhost:4566/000000000000/wager-transactions.fifo
EVENTS_QUEUE_URL=http://localhost:4566/000000000000/wager-events

# Keycloak
IDP_ISSUER_URL=http://localhost:8080/realms/jungle
IDP_JWKS_URL=http://localhost:8080/realms/jungle/protocol/openid-connect/certs
```

Os valores acima são exclusivamente para o ambiente local.

**Nunca committe credenciais reais.**

O `.gitignore` deve conter:

```gitignore
.env
.env.*
!.env.example
```

---

## 4. Inicialização do Ecossistema Local

O ambiente é provisionado automaticamente pelo Docker Compose.

Ao iniciar o ambiente:

1. PostgreSQL é iniciado;
2. LocalStack é iniciado;
3. as filas SQS são criadas automaticamente;
4. a fila principal FIFO é configurada com DLQ/redrive;
5. Keycloak é iniciado;
6. o Realm `jungle` é importado;
7. o client B2B de teste é disponibilizado;
8. a aplicação executa as migrations automaticamente.

Suba todo o ambiente:

```bash
docker compose up --build
```

Ou em background:

```bash
docker compose up --build -d
```

Para acompanhar os logs:

```bash
docker compose logs -f
```

Para acompanhar somente a aplicação:

```bash
docker compose logs -f api
```

---

## 5. Banco de Dados e Migrations

As migrations são executadas automaticamente durante o ciclo de inicialização da aplicação.

O serviço valida o estado do PostgreSQL e aplica as migrations necessárias antes de disponibilizar a API.

Os arquivos de migration ficam no diretório:

```text
migrations/
```

A migration inicial contém a estrutura necessária para:

* wallets;
* transações;
* ledger append-only;
* inbox;
* outbox;
* índices;
* constraints;
* controle de versão da carteira.

### Importante

A aplicação utiliza PostgreSQL como fonte persistente de verdade.

Nenhum mecanismo de memória local substitui:

* idempotência;
* ledger;
* inbox;
* outbox;
* saldo persistido.

---

## 6. Verificação e Testes

Execute a formatação:

```bash
gofmt -w .
```

Verifique possíveis problemas estáticos:

```bash
go vet ./...
```

Execute os testes:

```bash
go test ./...
```

Execute os testes com detector de race conditions:

```bash
go test -race ./...
```

Valide o Docker Compose:

```bash
docker compose config
```

Todos esses comandos devem terminar sem erros.

---

## 7. Testes de Integração / E2E

Os testes de integração utilizam infraestrutura real e não substituem PostgreSQL, SQS ou Keycloak por mocks.

Pré-requisito:

```bash
docker compose up --build -d
```

Aguarde:

* PostgreSQL healthy;
* LocalStack healthy;
* Keycloak healthy;
* filas SQS disponíveis.

### PowerShell

```powershell
$env:INTEGRATION="1"
go test -tags=integration ./internal/integration/ -count=1 -v -timeout 5m
```

### Bash

```bash
INTEGRATION=1 go test -tags=integration ./internal/integration/ -count=1 -v -timeout 5m
```

### Cenários cobertos

| Cenário                                             | Teste                                             |
| --------------------------------------------------- | ------------------------------------------------- |
| 2 apostas de `80.00` em saldo `100.00`              | `TestTwoBetsOf80OnBalance100`                     |
| 50 replays simultâneos da mesma transação           | `TestFiftySimultaneousReplaysSameTransaction`     |
| Redelivery SQS após commit                          | `TestSQSCrashRedeliveryAfterCommit`               |
| Envio real via LocalStack FIFO                      | `TestSQSSendAndProcessViaLocalStack`              |
| 2 publishers de Outbox com `SKIP LOCKED`            | `TestTwoConcurrentOutboxPublishersSkipLocked`     |
| 3 instâncias independentes                          | `TestThreeIndependentInstancesConcurrentBets`     |
| Restart preservando idempotência, pending e outbox  | `TestRestartPreservesIdempotencyPendingAndOutbox` |
| E2E HTTP + JWT + Keycloak + PostgreSQL + LocalStack | `TestE2E_PostgresKeycloakLocalStack`              |

### Limitação conhecida

Os testes de "3 instâncias" e "restart" simulam múltiplas instâncias no mesmo host, utilizando pools/use cases separados apontando para o mesmo PostgreSQL.

Eles não utilizam:

```bash
docker compose --scale
```

A persistência e a coordenação entre instâncias são, entretanto, realizadas através do PostgreSQL e dos mecanismos transacionais do sistema.

---

## 8. Autenticação

A API utiliza OAuth 2.0 / OIDC através do Keycloak.

Não existe token estático utilizado pela aplicação como mecanismo de autenticação.

O token deve ser emitido pelo Keycloak.

### Obter token

```bash
curl -X POST http://localhost:8080/realms/jungle/protocol/openid-connect/token \
  -H "Content-Type: application/x-www-form-urlencoded" \
  -d "grant_type=client_credentials" \
  -d "client_id=provider-a-service" \
  -d "client_secret=super-secret-token-provedor-a"
```

Copie o valor de:

```text
access_token
```

e utilize:

```text
Authorization: Bearer <TOKEN>
```

---

## 9. Guia Prático da API

### 9.1 Criar uma carteira

```bash
curl -X POST http://localhost:3000/wallets \
  -H "Authorization: Bearer <COLE_O_TOKEN_AQUI>" \
  -H "Content-Type: application/json" \
  -d '{
    "playerId": "player-marcos-123",
    "initialBalance": {
      "amount": "100.00",
      "currency": "BRL"
    }
  }'
```

A abertura da carteira gera:

* wallet;
* operação interna `OPENING`;
* lançamento correspondente no ledger;

tudo dentro da mesma transação.

---

## 10. Aposta — BET

Envie:

```bash
curl -X POST http://localhost:3000/wagering/transactions \
  -H "Authorization: Bearer <COLE_O_TOKEN_AQUI>" \
  -H "Idempotency-Key: provider-a:tx-rodada-001" \
  -H "Content-Type: application/json" \
  -d '{
    "providerId": "provider-a",
    "externalTransactionId": "tx-rodada-001",
    "playerId": "player-marcos-123",
    "walletId": "wal_player-marcos-123_brl",
    "roundId": "round-987",
    "gameId": "fortune-monkey",
    "kind": "BET",
    "money": {
      "amount": "80.00",
      "currency": "BRL"
    }
  }'
```

Com saldo inicial de `100.00`, o saldo esperado é:

```text
20.00 BRL
```

O valor monetário é processado com precisão decimal, sem utilização de `float`.

---

## 11. Idempotência

A mesma transação pode ser reenviada sem causar um segundo débito.

Repita a chamada anterior utilizando a mesma:

```text
Idempotency-Key
```

O resultado deverá indicar que a operação já havia sido processada.

A API também expõe:

```text
X-Cache-Lookup: HIT - Idempotent Replay
```

em um replay idempotente.

### Conflito de payload

Se o mesmo `Idempotency-Key` for utilizado com dados diferentes, a operação não é executada novamente.

Exemplo:

```json
{
  "money": {
    "amount": "10.00",
    "currency": "BRL"
  }
}
```

mantendo a mesma chave usada anteriormente.

Resultado esperado:

```text
HTTP 409 Conflict
```

Essa proteção impede que a mesma chave represente operações financeiras diferentes.

---

## 12. Consulta da Carteira

```bash
curl -X GET http://localhost:3000/wallets/wal_player-marcos-123_brl \
  -H "Authorization: Bearer <COLE_O_TOKEN_AQUI>"
```

---

## 13. Reconciliação

A reconciliação recalcula o saldo a partir do ledger persistido e compara o resultado com o saldo armazenado na wallet.

```bash
curl -X POST http://localhost:3000/wallets/wal_player-marcos-123_brl/reconciliation \
  -H "Authorization: Bearer <COLE_O_TOKEN_AQUI>"
```

Uma resposta consistente possui:

```json
{
  "consistent": true
}
```

A reconciliação permite detectar divergências entre:

* saldo armazenado;
* movimentos persistidos no ledger.

---

# 14. Consumidor SQS FIFO

O serviço possui um worker responsável pelo consumo assíncrono da fila:

```text
wager-transactions.fifo
```

O processamento utiliza:

* Inbox persistente;
* idempotência;
* transação PostgreSQL;
* ledger;
* atualização de saldo;
* Outbox;
* exclusão da mensagem somente após processamento bem-sucedido.

Em caso de falha antes da exclusão da mensagem, o SQS pode realizar redelivery.

O Inbox impede que a mesma mensagem gere novamente o movimento financeiro.

---

## 15. Testando SQS no LocalStack

### Importante para Windows / PowerShell

Evite passar um JSON complexo diretamente através de:

```powershell
--message-body '{ ... }'
```

porque o escaping entre PowerShell, Docker e shell do container pode remover as aspas internas do JSON.

Utilize um arquivo JSON.

### 15.1 Criar `send-message.json`

Na raiz do projeto:

```json
{
  "QueueUrl": "http://localhost:4566/000000000000/wager-transactions.fifo",
  "MessageBody": "{\"messageId\":\"msg-sqs-unique-001\",\"type\":\"WagerTransactionRequested\",\"occurredAt\":\"2026-09-21T20:15:00Z\",\"data\":{\"providerId\":\"provider-a\",\"externalTransactionId\":\"tx-assincrona-999\",\"idempotencyKey\":\"provider-a:tx-assincrona-999\",\"playerId\":\"player-marcos-123\",\"walletId\":\"wal_player-marcos-123_brl\",\"roundId\":\"round-987\",\"gameId\":\"fortune-monkey\",\"kind\":\"WIN\",\"money\":{\"amount\":\"50.00\",\"currency\":\"BRL\"}}}",
  "MessageGroupId": "player-marcos-group",
  "MessageDeduplicationId": "sqs-dedup-001"
}
```

### 15.2 Copiar o arquivo para o LocalStack

PowerShell:

```powershell
docker cp .\send-message.json jungle_localstack:/tmp/send-message.json
```

### 15.3 Enviar

```powershell
docker exec jungle_localstack awslocal sqs send-message --cli-input-json file:///tmp/send-message.json
```

O LocalStack deverá retornar:

```text
MessageId
SequenceNumber
```

### 15.4 Verificar o processamento

Observe os logs da aplicação.

Após o processamento, consulte a carteira:

```powershell
curl.exe http://localhost:3000/wallets/wal_player-marcos-123_brl `
  -H "Authorization: Bearer <COLE_O_TOKEN_AQUI>"
```

O saldo deverá refletir o crédito do `WIN`.

---

## 16. Modelo de Mensagem SQS

O envelope esperado possui a seguinte estrutura:

```json
{
  "messageId": "msg-sqs-unique-001",
  "type": "WagerTransactionRequested",
  "occurredAt": "2026-09-21T20:15:00Z",
  "data": {
    "providerId": "provider-a",
    "externalTransactionId": "tx-assincrona-999",
    "idempotencyKey": "provider-a:tx-assincrona-999",
    "playerId": "player-marcos-123",
    "walletId": "wal_player-marcos-123_brl",
    "roundId": "round-987",
    "gameId": "fortune-monkey",
    "kind": "WIN",
    "money": {
      "amount": "50.00",
      "currency": "BRL"
    }
  }
}
```

Tipos de transação suportados:

```text
BET
WIN
LOSS
REFUND
ROLLBACK
```

---

## 17. Concorrência e Integridade Financeira

As operações financeiras são executadas de forma transacional.

A carteira utiliza mecanismos de concorrência no PostgreSQL, incluindo:

* `SELECT ... FOR UPDATE`;
* controle de versão;
* atualização condicional;
* constraints;
* ledger append-only.

Isso permite evitar:

* saldo negativo;
* débito duplicado;
* movimentos duplicados;
* perda de idempotência;
* inconsistências entre wallet e ledger.

Duas apostas concorrentes sobre uma carteira com saldo insuficiente não podem consumir o mesmo saldo.

---

## 18. Inbox / Outbox

### Inbox

O Inbox registra mensagens recebidas de consumidores.

A gravação do Inbox e a operação financeira são coordenadas dentro da mesma transação PostgreSQL.

Assim, o processamento de uma mensagem e seu efeito financeiro não dependem de memória local.

### Outbox

Eventos de integração são gravados na Outbox dentro da mesma transação que realiza a operação financeira.

A publicação para SQS acontece posteriormente.

Isso evita publicar um evento antes de a operação financeira estar efetivamente commitada.

Publishers concorrentes utilizam locking apropriado, incluindo:

```sql
FOR UPDATE SKIP LOCKED
```

---

## 19. Reversões e Referências Pendentes

Operações como:

```text
REFUND
ROLLBACK
```

podem depender de uma transação anterior.

Quando a referência ainda não está disponível, o sistema mantém estado persistente de pendência em vez de depender de memória.

Um worker de recuperação pode tentar novamente posteriormente.

Isso permite recuperação após restart da aplicação.

---

## 20. Observabilidade

A aplicação disponibiliza mecanismos básicos de observabilidade e métricas para:

* operações processadas;
* erros de SQS;
* processamento de mensagens;
* comportamento dos workers.

Os logs permitem acompanhar:

* inicialização;
* consumo SQS;
* processamento;
* erros;
* shutdown.

---

## 21. Shutdown

Os workers são integrados ao lifecycle do Uber Fx.

Durante o encerramento da aplicação:

1. o recebimento de novas mensagens é interrompido;
2. workers são encerrados de forma coordenada;
3. o processo aguarda o encerramento dos workers dentro do timeout configurado.

Mensagens não removidas do SQS permanecem disponíveis para redelivery conforme as regras de visibilidade e redrive.

---

## 22. Validação Local Realizada

O fluxo principal foi validado utilizando PostgreSQL, Keycloak e LocalStack.

### Fluxo HTTP

Foi validado:

* criação de carteira com saldo inicial;
* BET de `80.00` sobre saldo de `100.00`;
* saldo resultante de `20.00`;
* replay da mesma transação sem novo débito;
* conflito de payload com HTTP `409`;
* reconciliação entre saldo armazenado e ledger.

### Fluxo SQS

Foi validado:

* envio de mensagem real para LocalStack;
* consumo pelo worker Go;
* processamento de `WIN`;
* atualização do saldo;
* redelivery de payload inválido;
* exclusão da mensagem após processamento bem-sucedido.

Durante o teste foi identificado que, no PowerShell, passar JSON diretamente por `docker exec` podia remover as aspas internas do payload. O teste foi corrigido utilizando um arquivo JSON enviado ao container.

### Verificações automatizadas

Foram executados com sucesso:

```bash
gofmt -w .
go test ./...
go test -race ./...
go vet ./...
docker compose config
```

---

## 23. Segurança

O projeto não deve utilizar:

* tokens estáticos como mecanismo de autenticação;
* credenciais reais no `.env`;
* memória como mecanismo de idempotência;
* mocks substituindo integralmente PostgreSQL, SQS ou Keycloak nos testes de integração.

A autenticação utiliza Keycloak/OIDC e as operações são associadas ao `providerId` autenticado.

---

## 24. Estrutura Geral

Principais componentes:

```text
cmd/
  api/

internal/
  domain/
  usecase/
  repository/
  infrastructure/
    repository/
    transport/
  config/

migrations/

docker-compose.yml

Dockerfile

.env.example

ARCHITECTURE.md

README.md
```

---

## 25. Execução Completa

Para reproduzir o projeto a partir de um checkout limpo:

### 1. Configurar ambiente

```powershell
Copy-Item .env.example .env
```

### 2. Subir infraestrutura

```powershell
docker compose up --build -d
```

### 3. Verificar containers

```powershell
docker compose ps
```

### 4. Executar testes

```powershell
go test ./...
go test -race ./...
go vet ./...
```

### 5. Validar Compose

```powershell
docker compose config
```

### 6. Executar integração

```powershell
$env:INTEGRATION="1"
go test -tags=integration ./internal/integration/ -count=1 -v -timeout 5m
```

### 7. Interagir com a API

Obtenha um token Keycloak e execute os exemplos das seções anteriores.

---

## 26. Limitações Conhecidas

As principais limitações conhecidas são:

1. Os testes de múltiplas instâncias são simulados no mesmo host através de pools/use cases separados.
2. O ambiente local utiliza LocalStack em vez da infraestrutura AWS real.
3. O Keycloak local é utilizado como provedor OIDC para desenvolvimento e testes.
4. O Compose local é voltado para reprodução do ambiente de desenvolvimento e avaliação técnica.

Essas limitações não substituem os mecanismos de persistência, concorrência e idempotência implementados no PostgreSQL.

---

## 27. Entrega

O projeto deve ser entregue contendo:

* código-fonte;
* migrations;
* Docker Compose;
* Dockerfile;
* `.env.example`;
* README;
* `ARCHITECTURE.md`;
* testes unitários;
* testes de integração;
* configuração do Keycloak;
* configuração do LocalStack/SQS.

Antes da entrega, executar:

```bash
gofmt -w .
go test ./...
go test -race ./...
go vet ./...
docker compose config
```

O arquivo `.env` local não deve ser versionado.

---

## 28. Licença

Projeto desenvolvido para fins de avaliação técnica.
