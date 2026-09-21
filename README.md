# Jungle Gaming — Distributed Wagering Ledger Service

Este é um microserviço financeiro B2B de altíssima confiabilidade e resiliência transacional, desenvolvido em **Go** com suporte a sistemas distribuídos. O ecossistema processa operações financeiras de provedores de jogos de azar (iGaming), garantindo precisão monetária absoluta, idempotência persistente, integridade de ledger append-only e isolamento estrito entre parceiros.

---

## 1. Pré-requisitos

Certifique-se de ter as seguintes ferramentas instaladas em sua máquina local:
*   **Go**: Versão 1.22 ou superior (declarada no `go.mod` e `Dockerfile`)
*   **Docker** e **Docker Compose**
*   Um cliente HTTP (ex: `curl`, Postman, Insomnia)

---

## 2. Variáveis de Ambiente (`.env`)

Crie um arquivo chamado `.env` na raiz do projeto. Você pode copiar os valores do modelo abaixo, configurados para se conectar de forma nativa e automática com o ambiente Docker unificado:

```env
# Configurações de Regras de Negócio
SUPPORTED_CURRENCIES=BRL,USD,EUR
INITIAL_WALLET_VERSION=1

# Persistência: PostgreSQL Local
DB_HOST=localhost
DB_PORT=5432
DB_USER=jungle_user
DB_PASSWORD=jungle_password
DB_NAME=jungle_wagering_ledger
DB_SSLMODE=disable

# Mensageria: AWS SQS Local (LocalStack)
AWS_REGION=us-east-1
SQS_ENDPOINT=http://localhost:4566
WAGER_QUEUE_URL=http://localhost:4566/000000000000/wager-transactions.fifo

# Segurança: Provedor de Identidade Externo (Keycloak)
IDP_ISSUER_URL=http://localhost:8080/realms/jungle
IDP_JWKS_URL=http://localhost:8080/realms/jungle/protocol/openid-connect/certs
```

---

## 3. Inicialização do Ecossistema Local

O ambiente local foi desenhado para ser **100% auto-provisionado**. No momento do boot do Docker Compose, os seguintes passos ocorrem em segundo plano:
1. O PostgreSQL sobe e cria o banco de dados.
2. O LocalStack é executado e o script `init-sqs.sh` cria automaticamente a fila FIFO principal e a fila de mensagens mortas (DLQ), atrelando as políticas de redrive.
3. O Keycloak é inicializado e importa o Realm `jungle` pré-registrando um Client de teste B2B.

Execute o comando oficial abaixo na raiz do seu projeto para subir toda a infraestrutura:

```sh
docker compose up --build
```

---

## 4. Evolução do Banco de Dados (Migrations)

A aplicação Go utiliza o mecanismo automatizado de migrations sincronizado pelo **Uber Fx Lifecycle** [file: 1]. Ao rodar a aplicação através do `cmd/api/main.go`, o sistema detectará o diretório `/migrations`, validará o estado do PostgreSQL e aplicará de forma automática todas as tabelas, índices e triggers de segurança antes de abrir a porta de rede do servidor HTTP [file: 1].

Para verificar ou forçar uma execução manual isolada via CLI, certifique-se de que os arquivos `000001_init_schema.up.sql` e `000001_init_schema.down.sql` encontram-se no diretório correspondente.

---

## 5. Comandos de Verificação e Testes

O regulamento exige verificações explícitas contra corridas de dados (*data races*) e checagens estritas do compilador [file: 1]. Use os comandos abaixo:

```sh
# Executa a validação estrita de tipos do compilador Go
go vet ./...

# Executa todos os testes unitários do Domínio
go test ./...

# Executa testes unitários com checagem ativa contra condições de corrida de memória
go test -race ./...
```

---

## 🚀 6. Guia Prático de Chamadas da API (Roteiro de Teste)

Para simular o comportamento da banca técnica e interagir com o sistema B2B, utilize o roteiro de comandos `curl` abaixo diretamente no terminal da sua máquina:

### Passo A: Obter o Token de Acesso do Provedor (Keycloak)
O microserviço exige autenticação estrita baseada em padrões OAuth 2.0 [file: 1]. Dispare a chamada abaixo para fingir ser o servidor do **Provedor A** solicitando um token de acesso para o Keycloak:

```sh
curl -X POST http://localhost:8080/realms/jungle/protocol/openid-connect/token \
  -H "Content-Type: application/x-www-form-urlencoded" \
  -d "grant_type=client_credentials" \
  -d "client_id=provider-a-service" \
  -d "client_secret=super-secret-token-provedor-a"
```
*Copie a string gigante do campo `access_token` retornada no JSON para utilizar nos cabeçalhos dos próximos passos.*

---

### Passo B: Abertura de Carteira (`POST /wallets`)
Cria a carteira e a conta financeira de um jogador. O Caso de Uso transacional gera o registro interno do tipo `OPENING` e seu lançamento correspondente de crédito no Ledger Append-Only no mesmo commit [file: 1].

```sh
curl -X POST http://localhost:3000/wallets \
  -H "Authorization: Bearer <COLE_O_TOKEN_AQUI>" \
  -H "Content-Type: application/json" \
  -d '{
    "playerId": "player-marcos-123",
    "initialBalance": { "amount": "100.00", "currency": "BRL" }
  }'
```

---

### Passo C: Enviar uma Aposta (`POST /wagering/transactions` - Tipo BET)
Simula uma cobrança de aposta realizada em um slot de cassino. O sistema travará a carteira com **Lock Pessimista**, validará os fundos e aplicará o débito [file: 1].

```sh
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
    "money": { "amount": "80.00", "currency": "BRL" }
  }'
```
*Após essa chamada, o saldo da carteira cairá com precisão matemática exata para R\$ 20.00.*

---

### Passo D: Testar a Idempotência e Detecção de Conflitos
1. **Replay Legítimo**: Repita a chamada do **Passo C** com os mesmos dados. O sistema não cobrará o saldo novamente, retornará o status original `PROCESSED` e marcará o cabeçalho Customizado `X-Cache-Lookup: HIT - Idempotent Replay` [file: 1].
2. **Conflito de Payload**: Repita a chamada mudando o valor da aposta de `80.00` para `10.00`, mantendo a mesma `Idempotency-Key`. A API interceptará o ataque, validará que o hash canônico SHA-256 não bate e retornará um erro **HTTP 409 (Conflict)** [file: 1].

---

### Passo E: Consultar Saldo Atualizado (`GET /wallets/:id`)
Executa uma leitura simples na tabela de carteiras sem onerar o banco com locks de escrita desnecessários.

```sh
curl -X GET http://localhost:3000/wallets/wal_player-marcos-123_brl \
  -H "Authorization: Bearer <COLE_O_TOKEN_AQUI>"
```

---

### Passo F: Executar a Reconciliação e Auditoria (`POST /wallets/:id/reconciliation`)
O endpoint Reconstrói o saldo computando em tempo real todos os registros de créditos e débitos armazenados na tabela `wallet_ledger_entries` e compara o balanço contra o saldo atual da carteira, reportando divergências de forma auditável e transparente [file: 1].

```sh
curl -X POST http://localhost:3000/wallets/wal_player-marcos-123_brl/reconciliation \
  -H "Authorization: Bearer <COLE_O_TOKEN_AQUI>"
```

---

## 📡 7. Testando o Consumidor SQS FIFO Assíncrono

Para enviar mensagens diretamente para a fila SQS gerenciada pelo LocalStack local e ver o worker em background processar a transação e persistir na `Inbox`, execute este comando no terminal utilizando a ferramenta `awslocal` ou o próprio container:

```sh
docker exec -it jungle_localstack awslocal sqs send-message \
  --queue-url http://localhost:4566/000000000000/wager-transactions.fifo \
  --message-group-id "player-marcos-group" \
  --message-deduplication-id "sqs-dedup-001" \
  --message-body '{
    "messageId": "msg-sqs-unique-001",
    "type": "WagerTransactionRequested",
    "occurredAt": "2026-09-08T12:00:00.000Z",
    "data": {
      "providerId": "provider-a",
      "externalTransactionId": "tx-assincrona-999",
      "idempotencyKey": "provider-a:tx-assincrona-999",
      "playerId": "player-marcos-123",
      "walletId": "wal_player-marcos-123_brl",
      "roundId": "round-987",
      "gameId": "fortune-monkey",
      "kind": "WIN",
      "money": { "amount": "50.00", "currency": "BRL" }
    }
  }'
```
Você verá nos logs da aplicação Go o worker capturando a mensagem, efetuando o commit atômico e removendo o envelope da fila com segurança [file: 1]!
