FROM debian:bookworm-slim
RUN apt-get update -qq && apt-get install -y -qq python3 ca-certificates curl ripgrep git && rm -rf /var/lib/apt/lists/*
WORKDIR /app
