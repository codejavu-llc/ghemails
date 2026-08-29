# syntax=docker/dockerfile:1
FROM golang:1.26.6-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=dev
ARG COMMIT=none
ARG BUILD_DATE=unknown
RUN CGO_ENABLED=0 go build -trimpath \
    -ldflags "-s -w -X github.com/codejavu-llc/ghemails/internal/app.Version=${VERSION} -X github.com/codejavu-llc/ghemails/internal/app.Commit=${COMMIT} -X github.com/codejavu-llc/ghemails/internal/app.Date=${BUILD_DATE}" \
    -o /out/ghemails .

FROM alpine:3.22
RUN apk add --no-cache ca-certificates git && addgroup -S ghemails && adduser -S -G ghemails ghemails
COPY --from=build /out/ghemails /usr/local/bin/ghemails
USER ghemails
ENTRYPOINT ["ghemails"]
CMD ["--help"]
