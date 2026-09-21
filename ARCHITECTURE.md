# Decisões de Arquitetura e Engenharia de Sistemas — Ledger Wagering Service

Este documento detalha as decisões técnicas, escolhas de design e abordagens de resiliência adotadas para resolver o desafio de processamento distribuído de transações e ledger financeiro.

## 1. Precisão Monetária e Design do Tipo `Money`
* **Abordagem:** Proibição absoluta do uso de tipos de ponto flutuante (`float32` ou `float64`) em qualquer etapa do ciclo de vida do software para mitigar riscos de arredondamentos silenciosos e erros de truncamento aritmético.
* **Implementação:** O objeto de domínio `Money` foi modelado como um **Value Object imutável** utilizando escala fixa baseada em inteiros de 64 bits (`int64`), onde o valor representa as unidades mínimas da moeda (centavos: `25.00` é processado e persistido estritamente como `2500`).
* **Blindagem:** O parsing de strings decimais é validado por uma expressão regular estrita (`^[0-9]+\.[0-9]{2}$`) que rejeita escalas excedentes, dízimas ou notação científica. Todas as operações aritméticas (`Add`, `Subtract`, `Negate`) validam e bloqueiam *overflows* numéricos contra os limites físicos do `math.MaxInt64`.

## 2. Coordenação de Concorrência Distribuída e Lost Updates
* **O Desafio:** Garantir consistência financeira com três instâncias da aplicação rodando em paralelo e disputando saldos concorrentes da mesma carteira (Cenário: Saldo R\$ 100 recebendo duas apostas simultâneas de R\$ 80).
* **Solução:** Adotou-se o modelo de **Lock Pessimista (`Pessimistic Locking`)** diretamente no nível da linha do banco de dados PostgreSQL através da instrução `SELECT ... FOR UPDATE`.
* **Justificativa:** Travar o estado na memória da aplicação (usando `sync.Mutex`) falharia em um ecossistema distribuído com múltiplos nós. O `SELECT FOR UPDATE` enfileira sequencialmente as transações que pertencem à *mesma carteira*, garantindo que a segunda aposta leia o saldo atualizado e seja rejeitada por fundos insuficientes, enquanto carteiras de jogadores diferentes avançam em paralelo com máxima performance (sem locks globais).

## 3. Idempotência Persistente e Integridade do Ledger
* **Deduplicação de Payload:** Para garantir que a mesma operação não seja processada duas vezes, o banco impõe uma restrição de unicidade composta (`UNIQUE`) sobre os campos `(provider_id, external_transaction_id)`.
* **JSON Canônico:** Ao receber uma requisição, o sistema calcula um hash SHA-256 determinístico sobre um JSON canônico ordenado por chaves alfabéticas dos dados de negócio. Se uma chave de idempotência for reutilizada com dados diferentes, o sistema detecta a divergência de hash e retorna um conflito `HTTP 409`.
* **Ledger Append-Only:** O histórico financeiro é estritamente imutável. Para garantir isso no nível físico e incontestável, foi acoplada uma `TRIGGER` em PL/pgSQL na tabela `wallet_ledger_entries` que aborta e bloqueia qualquer instrução de `UPDATE` ou `DELETE`.

## 4. Mensageria Resiliente com Padrões Inbox e Transactional Outbox
* **Exactly-Once Lógico (Inbox):** O consumidor do AWS SQS FIFO realiza a inserção do `message_id` na tabela `inbox_messages` dentro da mesma transação SQL que altera o saldo do jogador. Se uma mensagem for reentregue pelo comportamento *at-least-once* do broker, a colisão de chave primária impede o reprocessamento lógico.
* **Transactional Outbox:** Eventos de integração não são disparados diretamente de dentro dos Casos de Uso (o que causaria inconsistência se o banco fizesse rollback após o disparo). Os eventos são gravados na tabela `outbox_events` de forma atômica no mesmo commit da carteira.
* **Disputa por Lotes (SKIP LOCKED):** O worker em background `OutboxWorker` realiza varreduras periódicas utilizando `SELECT ... FOR UPDATE SKIP LOCKED`. Isso permite que as múltiplas instâncias da aplicação processem lotes de eventos pendentes concorrentemente sem causar concorrência de travamento (*deadlocks*) entre si.

## 5. Autenticação e Isolamento de Provedores via OIDC Keycloak
* **Modelo:** Integração real com Keycloak utilizando o fluxo B2B de `client_credentials`.
* **Segurança:** O `AuthMiddleware` intercepta os cabeçalhos de rede, valida a assinatura criptográfica dos tokens JWT emitidos pelo Keycloak e extrai a claim `azp`/`client_id`.
* **Isolamento Estrito:** A identidade autenticada determina de forma mandatória o `providerId` no contexto da requisição (`Context`). O sistema rejeita explicitamente qualquer tentativa de um provedor consultar, apostar ou executar replays em nome de outra operadora de jogos.
