# quick

Hosting interno in stile [Quick di Shopify](https://shopify.engineering/quick):
pubblichi una cartella di HTML/asset e ottieni `<nome>.<tuo-dominio>`. Di default
un sito è visibile solo agli account del dominio aziendale (SSO Google), ma puoi
aprirlo al pubblico o proteggerlo con un codice, e bloccarne la sovrascrittura.

Tutto è configurabile da variabili d'ambiente: nessun dominio o credenziale è
cablato nel codice. Lo storage può essere locale o object storage S3-compatibile.

## Pubblicare un sito

```bash
export QUICK_SERVER=https://quick.example.com   # una volta (o usa --server)
quick login                                     # login Google nel browser
quick deploy ./ilmiosito --name foo             # -> https://foo.quick.example.com
```

Senza `--name` usa il nome della cartella. Il deploy carica l'intera cartella e
sovrascrive il sito (i file rimossi spariscono). Sottodomini nuovi sono istantanei
(il wildcard copre già il certificato). La CLI si auto-configura: da `--server`
(o `QUICK_SERVER`) chiede a `GET <server>/api/config` client OAuth e domini.

## Visibilità e lock

```bash
quick publish   foo            # aperto a chiunque, niente SSO
quick unpublish foo            # torna dietro l'SSO aziendale (default)
quick private   foo            # accesso con codice (lo genera e te lo stampa)
quick private   foo --code abc # accesso con codice scelto da te
quick lock      foo            # da ora solo tu puoi sovrascrivere foo
quick unlock    foo
```

Il cambio di visibilità è **istantaneo** (solo un file di metadata). La decisione
di accesso la prende `quick-server`: pubblico → servito sempre; codice → pagina di
inserimento codice, poi un cookie firmato vale 7 giorni; SSO → verifica la sessione
Google via oauth2-proxy. Il **lock** registra te (dalla tua identità Google) come
owner: gli altri non possono più sovrascrivere né cambiare policy finché non fai
`unlock`.

## Architettura

```
browser ──https──> coolify-proxy (caddy-docker-proxy, wildcard TLS via DNS-01)
                     │  label su quick-server:  *.<BASE_DOMAIN> -> reverse_proxy quick-server:8080
                     ▼
                 quick-server (UNICO front, smista per path):
                   /api/health|config|deploy|site/<n>/policy
                   /oauth2/*   -> reverse proxy a oauth2-proxy (SSO Google)
                   /__quick/*  -> pagina codice
                   resto       -> policy (public/code/sso) + serve dallo Storage
                 Storage: local (bind mount) | S3-compatibile (stateless)

CLI quick ── login Google PKCE (loopback) ──> ID token ──> POST /api/deploy | /api/site/.../policy
```

Il proxy fa solo `reverse_proxy` verso quick-server: niente `file_server` né file
in `/dynamic`. Il routing vive nelle **label** del container (reload graceful, nessun
restart del proxy). Per il modello "backend" alla Quick (API condivise `quick.db`/
`quick.storage`/… chiamate dal frontend) il seam è pronto — namespace riservato
same-origin, identità già risolta dall'SSO, storage astratto — ma non è implementato.

## Componenti

| Path | Cosa |
|---|---|
| `cmd/quick/` | CLI: `login` (PKCE), `deploy`, `publish`/`private`/`lock`; si auto-configura da `/api/config` |
| `cmd/quick-server/` | Front unico: serve i siti, policy/gate, deploy, `/oauth2/*`, `/api/config` |
| `internal/quick/` | Contratto condiviso CLI↔server (DTO, validazione nomi, modi di accesso) |
| `internal/storage/` | Backend storage: `local` (FS) e `s3` (minio-go) |
| `docker-compose.yaml` | Stack per Coolify (label Caddy + env) |
| oauth2-proxy (env `OAUTH2_PROXY_*`) | SSO Google |

## Configurazione (env)

Vedi `.env.example`. In sintesi: `QUICK_BASE_DOMAIN`, `QUICK_ALLOWED_DOMAINS` (uno, lista `a,b`, o `*`),
`GOOGLE_CLIENT_ID/SECRET` (client OAuth **Web** per oauth2-proxy), `COOKIE_SECRET`,
`QUICK_META_SECRET`, `QUICK_STORAGE`=`local|s3` (+ `QUICK_S3_*`).

Client OAuth della CLI (`QUICK_CLI_CLIENT_ID` / `QUICK_CLI_CLIENT_SECRET`): due modi
- **Desktop app** → imposta solo l'ID; la CLI usa PKCE senza secret.
- **riuso di un client Web** (anche lo stesso di oauth2-proxy) → imposta ID + secret;
  il secret viene servito alla CLI via `/api/config` (accettabile per il client loopback
  PKCE-bound). È il modo per riusare un client Web esistente senza cablare nulla.

## Deploy su Coolify (4.1.x)

1. Crea una risorsa **Docker Compose** dal repo git (Coolify builda `quick-server`).
2. Imposta env/secrets (vedi sopra) e, se `QUICK_STORAGE=local`, i due bind mount.
3. **Connect to Predefined Network → coolify** (così il proxy raggiunge il container).
4. `CF_API_TOKEN` deve essere nell'env del proxy (lo usa la label `caddy.tls.dns`).

Il routing è tutto nelle label: cambiare contenuto o policy non richiede toccare il
proxy. Il vecchio `quick.caddy` in `/dynamic` non serve più (va rimosso al cutover).

## Sviluppo locale

```bash
QUICK_DEV_NOAUTH=1 QUICK_BASE_DOMAIN=quick.localhost \
  QUICK_SITES_DIR=./sites QUICK_META_DIR=./meta QUICK_META_SECRET=dev \
  go run ./cmd/quick-server
```
`QUICK_DEV_NOAUTH=1` salta la verifica del token (solo locale). Per lo storage S3 si
testa con un MinIO in Docker (vedi `QUICK_S3_*`).
