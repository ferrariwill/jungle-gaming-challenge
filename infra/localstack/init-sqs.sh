echo "=========== Inicializando Filas SQS no LocalStack ==========="

awslocal sqs create-queue \
  --queue-name wager-transactions-dlq.fifo \
  --attributes FifoQueue=true,ContentBasedDeduplication=true

DLQ_ARN=$(awslocal sqs get-queue-attributes \
  --queue-url http://localhost:4566/000000000000/wager-transactions-dlq.fifo \
  --attribute-names QueueArn --query "Attributes.QueueArn" --output text)

awslocal sqs create-queue \
  --queue-name wager-transactions.fifo \
  --attributes "FifoQueue=true,ContentBasedDeduplication=true,RedrivePolicy={\"deadLetterTargetArn\":\"$DLQ_ARN\",\"maxReceiveCount\":3}"

echo "=========== Filas SQS Inicializadas com Sucesso ==========="