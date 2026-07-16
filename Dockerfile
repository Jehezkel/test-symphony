FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /product-app .
RUN mkdir /data

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /product-app /product-app
COPY --from=build --chown=nonroot:nonroot /data /data
EXPOSE 8080
ENV PORT=8080
ENV DATABASE_PATH=/data/app.db
VOLUME ["/data"]
ENTRYPOINT ["/product-app"]
