# Discordo

App de comunidades com chat, voz e transmissão em tempo real. Monorepo:

| Pasta | O quê | Estado |
|---|---|---|
| `apps/server` | Backend em Go: REST + gateway WebSocket, Postgres, MinIO/S3 | chat, servidores, cargos, DMs, amigos e anexos prontos; SFU de voz/vídeo e sinalização prontos (transmissão em camadas e LiveKit a fazer) |
| `apps/web` | App web (React + Vite), servido pelo próprio servidor Go | login, servidores, canais, chat, amigos, DMs, chamadas de voz, câmera e transmissão de tela (480p a 4K) |
| `docs/protocol.md` | Contrato entre cliente e servidor | |
| `deploy/` | Infra local (Postgres, MinIO) | |

## Rodando o servidor

Precisa de Go e Docker.

1. Crie um app em <https://discord.com/developers/applications> e, em OAuth2, adicione o redirect
   `http://localhost:8080/auth/discord/callback`.
2. ```sh
   export DISCORD_CLIENT_ID=... DISCORD_CLIENT_SECRET=...
   make run
   ```

`make test` sobe Postgres e MinIO e roda os testes (de ponta a ponta, com dois clientes em WebSocket).

## Configuração (variáveis de ambiente)

| Variável | Padrão | |
|---|---|---|
| `DATABASE_URL` | obrigatória | Postgres; as migrations rodam ao iniciar |
| `DISCORD_CLIENT_ID`, `DISCORD_CLIENT_SECRET` | obrigatórias | |
| `S3_ACCESS_KEY`, `S3_SECRET_KEY` | obrigatórias | MinIO ou qualquer serviço compatível com S3 |
| `S3_ENDPOINT` | `localhost:9000` | `make run` usa `localhost:59000`, a porta do compose |
| `S3_BUCKET` | `discordo` | criado ao iniciar se não existir |
| `S3_SECURE` | `false` | `true` para TLS |
| `HTTP_ADDR` | `:8080` | |
| `MEDIA_UDP_PORT` | `7882` | toda a mídia passa por esta porta UDP; libere-a no firewall |
| `MEDIA_PUBLIC_IP` | vazio | IP pelo qual os clientes alcançam o servidor; vazio só serve para desenvolvimento na mesma máquina/rede |
| `PUBLIC_URL` | `http://localhost:8080` | como o navegador alcança o servidor |

## Rodando o app web

Precisa de Node 22. Em desenvolvimento o Vite (porta 5173) repassa a API e o gateway para o servidor (8080),
então o login precisa voltar pelo Vite para o cookie de sessão ficar no endereço do app:

1. No app do Discord, adicione também o redirect `http://localhost:5173/auth/discord/callback`.
2. ```sh
   PUBLIC_URL=http://localhost:5173 make run   # num terminal
   make web                                     # noutro; abra http://localhost:5173
   ```

Em produção o build do app (`npm run build`) é embutido no binário do servidor (ver `apps/server/Dockerfile`).

### Transmissão de tela

Em uma chamada, escolha a resolução (480p a 4K) e 30 ou 60 fps e clique em "Transmitir tela".
O navegador codifica em H.264 com o encoder da placa de vídeo quando disponível e cai para software sozinho.

## Produção (Openship)

O servidor roda em um Openship auto-hospedado, a partir de `openship.json` e `compose.production.yaml` (servidor, Postgres e MinIO; só o servidor é acessível de fora).

Fica na plataforma, não no repositório:

| Onde | O quê |
|---|---|
| Variáveis do projeto | `PUBLIC_URL`, `MEDIA_PUBLIC_IP`, e como segredos `POSTGRES_PASSWORD`, `S3_SECRET_KEY`, `DISCORD_CLIENT_ID`, `DISCORD_CLIENT_SECRET` |
| Serviço `server` | exposto na porta 8080 com o domínio customizado |
| Host | porta UDP 7882 publicada (voz e vídeo); precisa estar liberada no firewall |

No app do Discord (Developer Portal → OAuth2), o redirect é `<PUBLIC_URL>/auth/discord/callback`.

Novo deploy, com a CLI (`npm i -g openship`) autenticada na instância:

```sh
openship deploy --project discordo --branch <branch> --watch
```
