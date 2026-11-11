# Discordo

App de comunidades com chat, voz e transmissão em tempo real. Monorepo:

| Pasta | O quê | Estado |
|---|---|---|
| `apps/server` | Backend em Go: REST + gateway WebSocket, Postgres, MinIO/S3 | chat, servidores, cargos, DMs, amigos e anexos prontos; SFU de voz/vídeo e sinalização prontos (transmissão em camadas e LiveKit a fazer) |
| `apps/windows` | Cliente nativo Windows (C# WinUI 3) | login, servidores, canais, chat em tempo real, chamadas de voz e transmissão de tela (480p a 4K); ainda não compilado em um Windows |
| `crates/media-core` | Motor de mídia em Rust: WebRTC, áudio (WASAPI + Opus), captura de tela e H.264 por GPU (Media Foundation) | transporte testado contra o SFU; áudio e vídeo checados só por tipos, ainda não executados em um Windows |
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

## Rodando o cliente Windows

Precisa de:

- Visual Studio 2022 com as cargas "Desenvolvimento de aplicativos WinUI" e "Desenvolvimento para desktop com C++" (esta inclui o CMake, usado para compilar o codec Opus);
- [Rust](https://rustup.rs) (o build do app compila o motor de mídia com `cargo`).

```powershell
$env:DISCORDO_SERVER = "http://localhost:8080"   # opcional; o padrão é https://discordo.arluck.com.br
dotnet run --project apps/windows/Discordo.App -p:Platform=x64
```

Ou abra `apps/windows/Discordo.sln` e rode o projeto `Discordo.App`.

### Transmissão de tela

Em uma chamada, escolha a resolução (480p a 4K) e 30 ou 60 fps e clique em "Transmitir tela" (monitor principal).
A codificação usa o encoder da placa de vídeo (NVENC, AMF ou Quick Sync); sem um, cai para o encoder de software e o app avisa.
Para testar esse fallback, defina `DISCORDO_FORCE_SW_ENCODE=1` antes de abrir o app.
No Gerenciador de Tarefas, aba Desempenho → GPU, o gráfico "Video Encode" mostra o encoder de hardware em uso.

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

O cliente Windows aponta para produção por padrão.
