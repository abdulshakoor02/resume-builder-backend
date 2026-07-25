FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates && rm -rf /var/lib/apt/lists/*
RUN mkdir -p /app
COPY app /app/
WORKDIR /app
EXPOSE 1100
CMD ["./app"]
