FROM node:22-alpine AS ui
WORKDIR /src/ui
COPY ui/package*.json ./
RUN npm ci
COPY ui/ ./
RUN npm run build

FROM golang:1.25-alpine AS go
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . ./
RUN CGO_ENABLED=0 go build -buildvcs=false -o /out/csm .

FROM alpine:3.21
COPY --from=go /out/csm /app/csm
COPY --from=ui /src/ui/dist /var/run/ko/ui/dist
USER 65532:65532
ENTRYPOINT ["/app/csm"]
