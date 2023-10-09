# Dockerfile
FROM alpine
COPY outsystemscc /outsystemscc
ENTRYPOINT ["/outsystemscc"]
