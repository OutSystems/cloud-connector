# Dockerfile
FROM alpine
COPY outsystemscc /app/
ENTRYPOINT ["/app/outsystemscc"]
