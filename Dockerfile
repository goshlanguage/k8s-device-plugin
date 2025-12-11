FROM golang:1.25 as build

WORKDIR /src

# Copy go.mod and go.sum first for caching
COPY go.mod go.sum ./
RUN go mod download

# Copy the entire project
COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -o device-plugin ./cmd/k8s-device-plugin/main.go

FROM scratch
COPY --from=build /src/device-plugin /device-plugin
ENTRYPOINT ["/device-plugin"]
