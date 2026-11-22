# Archive shipper for the production overlay: Postgres client tools (pg_basebackup, psql),
# age and the AWS CLI. The Postgres tag matches the server in compose.yml.
FROM golang:1.27 AS age
RUN CGO_ENABLED=0 GOBIN=/out go install filippo.io/age/cmd/age@v1.3.2

FROM postgres:18.6
# No CA bundle in the postgres image: psql sslmode=verify-* and any HTTPS client need one.
COPY --from=age /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=amazon/aws-cli:2.39.6 /usr/local/aws-cli /usr/local/aws-cli
RUN ln -s /usr/local/aws-cli/v2/current/bin/aws /usr/local/bin/aws
COPY --from=age /out/age /usr/local/bin/age
COPY walship.sh /walship.sh
ENTRYPOINT ["/walship.sh"]
CMD ["loop"]
