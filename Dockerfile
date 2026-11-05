FROM golang:1.25 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /lemontube ./cmd/lemontube

FROM python:3.13-slim
COPY --from=ghcr.io/astral-sh/uv:latest /uv /usr/local/bin/uv
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates ffmpeg && rm -rf /var/lib/apt/lists/* \
    && uv pip install --system --no-cache 'yt-dlp[default]'
COPY --from=node:22-slim /usr/local/bin/node /usr/local/bin/node
COPY --from=build /lemontube /usr/local/bin/lemontube
ENV STATE_DIR=/data LISTEN_ADDR=:8081
EXPOSE 8081
ENTRYPOINT ["lemontube"]
