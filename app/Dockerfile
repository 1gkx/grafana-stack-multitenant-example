FROM golang:alpine as builder
# Install git + SSL ca certificates
RUN apk update && apk add git && apk add ca-certificates
# Create appuser
WORKDIR /var/app
COPY . /var/app
#get dependancies
RUN go get -d -v
#build the binary
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /var/app/main
RUN chmod +x /var/app/main

# STEP 2 build a small image
# start from scratch
FROM alpine:3.20
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
# Copy our static executable
COPY --from=builder /var/app/main /opt/app/main
ENTRYPOINT ["/opt/app/main"]