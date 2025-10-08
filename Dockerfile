# Dockerfile
FROM alpine
RUN apk update && apk upgrade
COPY outsystemscc /app/
ENTRYPOINT ["/app/outsystemscc"]