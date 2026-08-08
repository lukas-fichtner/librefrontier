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

# Create a non-root user and group
RUN addgroup -S librefrontier && adduser -S librefrontier -G librefrontier

WORKDIR /app

# Copy binaries with ownership set to the non-root user
COPY --from=builder --chown=librefrontier:librefrontier /build/api .
COPY --from=builder --chown=librefrontier:librefrontier /build/gui .
COPY --chown=librefrontier:librefrontier gui/templates ./templates

# Switch to the non-root user
USER librefrontier

EXPOSE 8080 8081

ENV GIN_MODE=release

CMD ["sh", "-c", "./api & ./gui"]