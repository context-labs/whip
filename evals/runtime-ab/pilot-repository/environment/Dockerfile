FROM debian:bookworm-slim
RUN apt-get update -qq && apt-get install -y -qq python3 ca-certificates curl ripgrep git && rm -rf /var/lib/apt/lists/*
WORKDIR /app
COPY backoff.py /app/backoff.py
RUN git init -q && git config user.name Fixture && git config user.email fixture@example.invalid && git add . && git commit -qm initial
