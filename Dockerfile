# Build the urlshortener binary
FROM golang:1.24 as builder

WORKDIR /workspace
# Copy the Go Modules manifests
COPY go.mod go.mod
COPY go.sum go.sum
# cache deps before building and copying source so that we don't need to re-download as much
# and so that source changes don't invalidate our downloaded layer
RUN go mod download

# Copy the go source
COPY main.go main.go
COPY api/ api/
COPY controllers/ controllers/
COPY pkg/ pkg/
COPY docs/ docs/

# Build
# TODO switch back to original
#RUN CGO_ENABLED=0 GOOS=linux go build -a -o urlshortener main.go
RUN CGO_ENABLED=0 GOOS=linux go build -o urlshortener main.go

# Use distroless as minimal base image to package the urlshortener binary
# Refer to https://github.com/GoogleContainerTools/distroless for more details
# TODO: For production re-enable distroless!
#FROM gcr.io/distroless/static:nonroot
FROM alpine:latest
WORKDIR /
COPY --from=builder /workspace/urlshortener .
COPY html/ html/

USER 65532:65532

EXPOSE 8123

#ENTRYPOINT ["/urlshortener --bind-address=:8123"]
