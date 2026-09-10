FROM python@sha256:2f2e5a876c71a6757f55ec57f2add0225ddaf01c802a33fcc29073943f94d907
RUN apt-get update -qq && apt-get install -y -qq ca-certificates curl ripgrep git && rm -rf /var/lib/apt/lists/*
WORKDIR /app
RUN git init -q && git config user.email fixture@example.invalid && git config user.name Fixture && git commit --allow-empty -qm base
