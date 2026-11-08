FROM golang:1.27.3 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -o /app .

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates && rm -rf /var/lib/apt/lists/* && useradd --system --uid 10001 --create-home quiz && mkdir /data && chown quiz:quiz /data
COPY --from=build /app /usr/local/bin/quiz-bot
USER quiz
ENV HTTP_HOST=0.0.0.0 PORT=8080 DATA_PATH=/data/quiz.db
EXPOSE 8080
VOLUME ["/data"]
ENTRYPOINT ["/usr/local/bin/quiz-bot"]
