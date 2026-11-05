FROM golang:1.27 AS build
WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/quicticks ./server

FROM gcr.io/distroless/static-debian12
WORKDIR /app
COPY --from=build /out/quicticks /app/quicticks
COPY index.html styles.css app.js admin.html admin.js scan.html ./
EXPOSE 8080
ENTRYPOINT ["/app/quicticks"]