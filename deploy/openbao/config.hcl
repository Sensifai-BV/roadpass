# OpenBao for local stacks and E2E. Production: TLS on the listener, an HA
# storage backend, and auto-unseal (cloud KMS or an HSM) instead of init.sh.
storage "file" {
  path = "/openbao/file"
}
listener "tcp" {
  address     = "0.0.0.0:8200"
  tls_disable = true
}
api_addr      = "http://openbao:8200"
ui            = false
disable_mlock = true
