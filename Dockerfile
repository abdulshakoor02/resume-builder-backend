FROM alpine:3.21
RUN apk add --no-cache ca-certificates
COPY app /app
COPY .env /app/.env
WORKDIR /app
EXPOSE 1100
CMD ["./app"]
