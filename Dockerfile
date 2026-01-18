FROM golang:1.23-alpine AS builder

RUN apk update && apk add --no-cache git

WORKDIR /build
COPY go.mod ./
COPY . .
RUN go mod tidy

RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o /build/api ./api
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o /build/gui ./gui

FROM alpine:latest

RUN apk update && apk --no-cache add ca-certificates

WORKDIR /root/

COPY --from=builder /build/api .
COPY --from=builder /build/gui .
COPY gui/templates ./templates

EXPOSE 80 8080

CMD ["sh", "-c", "./api & ./gui"]
