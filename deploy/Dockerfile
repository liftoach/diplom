FROM golang:1.26.3-alpine3.23 AS build
WORKDIR /src
RUN apk add --no-cache make
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
COPY build/Makefile ./build/Makefile
ARG SERVICE
RUN make -f build/Makefile build-service SERVICE=${SERVICE}

FROM scratch
COPY --from=build /out/service /service
USER 65532:65532
ENTRYPOINT ["/service"]
