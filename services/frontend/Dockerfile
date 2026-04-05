FROM golang:1.26-alpine AS build
WORKDIR /src

COPY services/frontend/go.mod ./
RUN go mod download

COPY services/frontend/. ./
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/ledgerpay-frontend .

FROM alpine:3.22
WORKDIR /app

RUN addgroup -S ledgerpay && adduser -S ledgerpay -G ledgerpay
COPY --from=build /out/ledgerpay-frontend /app/ledgerpay-frontend

EXPOSE 8080
USER ledgerpay

CMD ["/app/ledgerpay-frontend"]
