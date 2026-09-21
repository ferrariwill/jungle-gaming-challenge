# =========================================================================
# ESTÁGIO 1: Compilação e Build da Aplicação
# =========================================================================
FROM golang:1.22-alpine AS builder

# Instala ferramentas essenciais de build do ecossistema Linux
RUN apk add --no-cache git ca-certificates tzdata

# Define o diretório de trabalho dentro do container do builder
WORKDIR /app

# Copia os arquivos de definição de dependências primeiro (aproveitamento de cache de camadas)
COPY go.mod go.sum ./

# Baixa e verifica as dependências do Go Modules conforme exigido no item 4
RUN go mod download && go mod verify

# Copia todo o código-fonte do projeto para dentro do builder
COPY . .

# Compila o binário de produção de forma estática
# CGO_ENABLED=0 desativa dependências dinâmicas do C facilitando portabilidade do container
# GOOS=linux garante que o binário gerado seja compatível com a arquitetura Linux do container final
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /app/jungle-api ./cmd/api/main.go

# =========================================================================
# ESTÁGIO 2: Container Final de Execução (Minimalista e Seguro)
# =========================================================================
FROM alpine:3.19

# Adiciona certificados SSL/TLS atualizados e fuso horário correto (Essencial para iGaming/Fintech)
RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app

# Copia os arquivos de migração necessários para o boot automatizado da aplicação (Item 4)
COPY --from=builder /app/migrations ./migrations

# Copia apenas o binário compilado e limpo do estágio anterior
COPY --from=builder /app/jungle-api .

# Expõe a porta de rede parametrizada no http_handler e main (Item 9)
EXPOSE 3000

# Executa o binário como ponto de entrada principal do container
ENTRY POINT ["./jungle-api"]
