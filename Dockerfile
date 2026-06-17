FROM golang:1.26.4 AS build

WORKDIR /go/src/app
COPY . .

ARG GOPROXY
RUN go mod download

RUN CGO_ENABLED=0 go build -o /go/bin/app ./cmd/app/main.go

FROM gcr.io/distroless/static-debian12
COPY --from=build /go/bin/app /
CMD ["/app"]
