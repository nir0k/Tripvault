# The one-shot service that fills the test stand with its examples through the
# public API. The fixtures are built into the image: the stand has no volumes.
FROM python:3.14.7-alpine3.24

WORKDIR /seed
COPY seed.py ./
COPY fixtures/ ./fixtures/

USER 65534
ENTRYPOINT ["python3", "/seed/seed.py"]
