# Discordo

App de comunidades com chat, voz e transmissão em tempo real. Monorepo:

| Pasta | O quê | Estado |
|---|---|---|
| `apps/server` | Backend em Go: REST + gateway WebSocket, Postgres, MinIO/S3 | chat, servidores, cargos, DMs, amigos e anexos prontos; voz/vídeo a fazer |
| `apps/windows` | Cliente nativo Windows (C# WinUI 3) | login, servidores, canais e chat em tempo real; ainda não compilado em um Windows |
| `crates/media-core` | Núcleo de mídia em Rust (captura e encode por GPU, WebRTC) | a fazer |
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
| `PUBLIC_URL` | `http://localhost:8080` | como o navegador alcança o servidor |

## Rodando o cliente Windows

Precisa do Visual Studio 2022 com a carga de trabalho "Desenvolvimento de aplicativos WinUI" (ou só o SDK do .NET 8).

```powershell
$env:DISCORDO_SERVER = "http://<ip do servidor>:8080"   # opcional; o padrão é http://localhost:8080
dotnet run --project apps/windows/Discordo.App -p:Platform=x64
```

Ou abra `apps/windows/Discordo.sln` e rode o projeto `Discordo.App`.
