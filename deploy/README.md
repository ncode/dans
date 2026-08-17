# Deployment examples

These examples preserve the supported production boundary:

```text
client --TLS--> trusted ingress --> private DANS --> PostgreSQL primary
                                             \--> private PowerDNS API
```

PostgreSQL and PowerDNS are external dependencies. The examples do not publish the DANS listener or PowerDNS API, and only DANS receives the PowerDNS key. Authoritative DNS on port 53 remains independent of DANS.

## Docker or Podman Compose

The Compose example expects existing `database-private` and `powerdns-control` networks. Attach the external PostgreSQL primary to the first and the PowerDNS API listener to the second. Do not publish the PowerDNS API port on the host. Its configured `api-allow-from` must also restrict clients to the DANS network address range.

Create the networks once, then run the example with absolute paths to secret files and a private PowerDNS URL:

```sh
docker network create --internal database-private
docker network create --internal powerdns-control

export DANS_IMAGE=ghcr.io/ncode/dans:RELEASE
export DANS_POWERDNS_URL=http://powerdns:8081
export DANS_DATABASE_SECRET_FILE=/secure/dans/database-url
export DANS_POWERDNS_SECRET_FILE=/secure/dans/powerdns-api-key
export DANS_TLS_CERT_FILE=/secure/dans/tls.crt
export DANS_TLS_KEY_FILE=/secure/dans/tls.key
docker compose --file deploy/docker/compose.yaml up -d
```

Use `podman network create --internal ...` and `podman compose --file deploy/docker/compose.yaml up -d` for Podman. Ensure the host secret files are readable by container UID 65532 without making them generally readable. The only published socket is the ingress's TLS port 443; DANS itself joins only private networks.

## Kubernetes

The manifest assumes:

- an ingress class named `trusted`, with its controller namespace labeled `networking.dans.io/trusted-ingress=true` and controller pods labeled `app.kubernetes.io/component=controller`;
- an external PostgreSQL primary in a namespace labeled `networking.dans.io/postgresql=true`, with pods labeled `app.kubernetes.io/name=postgresql`;
- PowerDNS in namespace `powerdns`, labeled at the namespace level with `networking.dans.io/powerdns=true` and at the pod level with `app.kubernetes.io/name=powerdns`;
- a PowerDNS API Service named `powerdns-api` on port 8081; and
- a TLS Secret named `dans-tls` in namespace `dans`.

Replace the image tag, host, service names, labels, and ports for the deployment. If PostgreSQL is a managed service outside the cluster, replace its namespace/pod egress selector with the provider's stable `ipBlock`; do not open unrestricted egress.

Create secret objects from files rather than placing plaintext in manifests:

```sh
kubectl create namespace dans --dry-run=client -o yaml | kubectl apply -f -
kubectl --namespace dans create secret generic dans-runtime \
  --from-file=database-url=/secure/dans/database-url \
  --from-file=powerdns-api-key=/secure/dans/powerdns-api-key
kubectl --namespace dans create secret tls dans-tls \
  --cert=/secure/dans/tls.crt --key=/secure/dans/tls.key
kubectl apply --filename deploy/kubernetes/dans.yaml
```

The pod runs as UID/GID 65532 with a read-only filesystem and no Linux capabilities. NetworkPolicy admits API traffic only from the trusted ingress and permits DANS egress only to DNS, PostgreSQL, and PowerDNS. The PowerDNS-side policy allows public authoritative DNS on port 53 but admits its HTTP API only from DANS. Adapt the selectors to the actual ingress and dependency labels before applying the policy.
