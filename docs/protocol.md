# Protocolo cliente ↔ servidor

A fonte da verdade são os tipos Go em `apps/server/internal`. Este documento é o resumo para quem escreve o cliente.

## Convenções

- JSON em tudo. IDs são UUIDv7 em texto: ordenar por ID é ordenar por data de criação.
- Autenticação: `Authorization: Bearer <token>`.
- Erros: `{"error": "..."}` com status 400 (inválido), 401, 403 (sem permissão), 404 (não existe **ou você não pode ver**), 409.
- Sucesso sem corpo responde 204.

## Login (Discord OAuth)

1. O app gera um `verifier` aleatório, passa a ouvir HTTP em `127.0.0.1:<porta livre>` e abre o navegador em
   `GET /auth/discord/login?port=<porta>&challenge=<base64url(sha256(verifier)) sem padding>`.
2. Depois do Discord, o navegador é redirecionado para `http://127.0.0.1:<porta>/?code=<código de uso único, 60 s>`.
3. O app troca: `POST /auth/token {"code", "verifier"}` → `{"token", "user"}`. O token vale 30 dias.
4. `POST /auth/logout` invalida o token.

## Gateway (tempo real)

WebSocket em `GET /gateway`. A primeira mensagem do cliente é `{"token": "..."}`. Toda mensagem do servidor é `{"t": TIPO, "d": DADOS}`.
O servidor envia pings WebSocket. Depois de autenticar, o cliente só envia mensagens de chamada, no formato `{"op": OP, "d": DADOS}` (seção "Voz e vídeo"). Ao reconectar, o `READY` substitui todo o estado local.
Eventos podem chegar repetidos: aplique por ID, de forma idempotente.

| Tipo | Dados | Quando |
|---|---|---|
| `READY` | `{user, guilds: [Snapshot], dms: [DM], relationships: [Relationship], voice_states: [VoiceState]}` | logo após autenticar |
| `GUILD_SYNC` | `Snapshot` | você entrou no servidor, ou algo estrutural mudou (servidor, canais, cargos, permissões). Substitua a cópia local |
| `GUILD_DELETE` | `{id}` | servidor apagado, ou você saiu / foi expulso / banido |
| `MEMBER_ADD` | `{guild_id, member}` | |
| `MEMBER_REMOVE` | `{guild_id, user_id}` | |
| `PRESENCE_UPDATE` | `{user_id, online}` | membros dos seus servidores e amigos |
| `MESSAGE_CREATE` / `MESSAGE_UPDATE` | `Message` | |
| `MESSAGE_DELETE` | `{id, channel_id}` | |
| `REACTION_ADD` / `REACTION_REMOVE` | `{channel_id, message_id, user_id, emoji}` | |
| `TYPING_START` | `{channel_id, user_id}` | considere "digitando" por ~8 s |
| `DM_CREATE` / `DM_UPDATE` | `DM` | |
| `DM_DELETE` | `{id}` | você saiu do grupo |
| `RELATIONSHIP_UPDATE` | `Relationship` | `status` 0 = remova da lista |
| `VOICE_STATE_UPDATE` | `VoiceState` | alguém entrou, saiu (`channel_id` vazio), mutou ou mudou o que transmite |
| `VOICE_READY` / `VOICE_ERROR` / `RTC_OFFER` / `RTC_ANSWER` | ver "Voz e vídeo" | só para a sessão que está na chamada |

### Tipos

```
User         {id, username, display_name, avatar_url}
Snapshot     {id, name, icon_url, owner_id, permissions, roles: [Role], channels: [Channel], members: [Member]}
             // só os canais que você pode ver; permissions = suas permissões no servidor
Role         {id, guild_id, name, color, permissions, position}   // id == guild_id é o @everyone
Member       {user, nickname, roles: [id], joined_at, online}
Channel      {id, guild_id, type, name, topic, position, overwrites: [Overwrite]}   // type 0 texto, 1 voz
Overwrite    {target_id, is_member, allow, deny}
Message      {id, channel_id, author: User, content, created_at, edited_at, mentions: [user id],
              mention_everyone, attachments: [Attachment], reactions: [{emoji, users: [id]}]}
Attachment   {id, filename, content_type, size}
DM           {id, type, name, recipients: [User]}   // type 2 conversa, 3 grupo
Relationship {user, status, online}   // 1 amigo, 2 pedido recebido, 3 pedido enviado, 4 bloqueado
VoiceState   {user_id, channel_id, guild_id?, muted, deafened, tracks: [nome]}
```

Menções vão no texto como `<@id-do-usuário>`; `@everyone` só conta para quem tem a permissão.

### Permissões (bits)

`1` ver canal · `2` enviar mensagens · `4` gerenciar mensagens · `8` anexar arquivos · `16` reagir · `32` mencionar everyone ·
`64` conectar (voz) · `128` falar · `256` transmitir vídeo · `512` criar convite · `1024` expulsar · `2048` banir ·
`4096` gerenciar canais · `8192` gerenciar cargos · `16384` gerenciar servidor · `32768` administrador.

O dono tem tudo. Ninguém mexe em cargo de posição igual ou acima da sua, nem concede permissão que não tem.

## REST

| Rota | Corpo | Resposta |
|---|---|---|
| `GET /users/@me` | | `User` |
| `POST /guilds` | `{name}` | `Snapshot` |
| `PATCH /guilds/{guild}` | `{name?, icon_url?}` | |
| `DELETE /guilds/{guild}` | | só o dono |
| `POST /guilds/{guild}/invites` | `{max_uses?, max_age?}` (segundos; 0 = sem limite) | `{code, ...}` |
| `POST /invites/{code}` | | `Snapshot` (entra no servidor) |
| `DELETE /guilds/{guild}/members/@me` | | sair |
| `DELETE /guilds/{guild}/members/{user}` | | expulsar |
| `PUT` / `DELETE /guilds/{guild}/bans/{user}` | | banir / desbanir |
| `PUT` / `DELETE /guilds/{guild}/members/{user}/roles/{role}` | | dar / tirar cargo |
| `POST /guilds/{guild}/roles` | `{name?, color?, permissions?, position?}` | `Role` |
| `PATCH` / `DELETE /guilds/{guild}/roles/{role}` | idem | `Role` |
| `POST /guilds/{guild}/channels` | `{name, type?, topic?, position?}` | `Channel` |
| `PATCH` / `DELETE /channels/{channel}` | `{name?, topic?, position?}` | `Channel` |
| `PUT` / `DELETE /channels/{channel}/overwrites/{target}` | `{is_member, allow, deny}` | |
| `GET /channels/{channel}/messages?before=&limit=` | | `[Message]`, mais novas primeiro (máx. 100) |
| `POST /channels/{channel}/messages` | `{content, attachments?: [id]}` | `Message` |
| `PATCH` / `DELETE /channels/{channel}/messages/{message}` | `{content}` | `Message` |
| `PUT` / `DELETE /channels/{channel}/messages/{message}/reactions/{emoji}` | | |
| `POST /channels/{channel}/typing` | | |
| `POST /channels/{channel}/attachments` | `multipart/form-data`, parte `file` (até 25 MB) | `Attachment` |
| `GET /attachments/{attachment}` | | o arquivo |
| `POST /relationships` | `{username}` | pede amizade (ou aceita o pedido pendente) |
| `PUT /relationships/{user}` | | aceita |
| `DELETE /relationships/{user}` | | desfaz amizade / recusa / cancela / desbloqueia |
| `PUT /blocks/{user}` | | bloqueia |
| `POST /dms` | `{recipient_id}` | `DM` (abre ou reaproveita) |
| `POST /dms/groups` | `{name?, recipient_ids}` (só amigos, até 10 pessoas) | `DM` |
| `PUT /dms/{channel}/recipients/{user}` | | adiciona ao grupo |
| `DELETE /dms/{channel}/recipients/@me` | | sai do grupo |

`{channel}` nas rotas de mensagens aceita canal de servidor ou DM.

## Voz e vídeo

Uma chamada acontece em um canal de voz ou em qualquer DM. Cada usuário está em no máximo uma, presa à conexão do gateway que entrou (cair a conexão encerra a chamada).

Cliente → servidor (`{"op", "d"}`):

| op | d | |
|---|---|---|
| `voice_join` | `{channel_id}` | exige a permissão conectar; sai de qualquer outra chamada |
| `voice_leave` | | |
| `voice_state` | `{muted, deafened}` | só informativo para os outros; quem muta de verdade é o cliente |
| `rtc_offer` | `{sdp}` | oferta da conexão de **envio** |
| `rtc_answer` | `{sdp}` | resposta à oferta da conexão de **recepção** |
| `rtc_subscribe` | `{user_id, track, on}` | começa/para de receber `camera` ou `screen` de alguém |

Servidor → cliente: `VOICE_READY {channel_id, connection: {kind}}`, `VOICE_ERROR {op, error}`, `RTC_ANSWER {sdp}`, `RTC_OFFER {sdp}`.

### Mídia (`connection.kind == "pion"`)

O cliente mantém **duas** conexões WebRTC com o servidor, para as ofertas nunca colidirem:

- **Envio**: o cliente cria, adiciona suas faixas e manda `rtc_offer` sempre que o que ele envia muda; recebe `RTC_ANSWER`.
- **Recepção**: o servidor manda `RTC_OFFER` sempre que o que o cliente recebe muda; o cliente responde `rtc_answer`.

Sem trickle ICE: espere a coleta de candidatos terminar antes de enviar um SDP (os do servidor já vêm completos).

O **ID da faixa** diz o que ela é: `mic`, `screen-audio` (áudio, Opus), `camera`, `screen` (vídeo, H.264). Outros nomes são ignorados.
`mic` exige a permissão falar; as demais, transmitir vídeo. Nas faixas recebidas, o ID do *stream* é o ID do usuário dono.

Áudio chega de todos automaticamente. Vídeo só depois de `rtc_subscribe`: use `tracks` do `VoiceState` para saber quem está transmitindo o quê.
