# Build stage
FROM --platform=$BUILDPLATFORM golang:1.27-alpine AS build
ARG TARGETOS
ARG TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -ldflags "-s -w" -o /out/dinghy .

# Runtime stage
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/dinghy /usr/local/bin/dinghy
EXPOSE 8081
ENTRYPOINT ["/usr/local/bin/dinghy"]
