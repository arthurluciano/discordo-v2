# Protocolo cliente ↔ servidor

A fonte da verdade são os tipos Go em `apps/server/internal`. Este documento é o resumo para quem escreve o cliente.

## Convenções

- JSON em tudo. IDs são UUIDv7 em texto: ordenar por ID é ordenar por data de criação.
- Autenticação: o cookie de sessão `discordo_session` (HttpOnly) ou `Authorization: Bearer <token>`.
  Com o cookie, requisições que não são GET precisam do header `Origin` com o mesmo host do servidor (CSRF).
- Erros: `{"error": "..."}` com status 400 (inválido), 401, 403 (sem permissão), 404 (não existe **ou você não pode ver**), 409.
- Sucesso sem corpo responde 204.

## Login (Discord OAuth)

1. O navegador abre `GET /auth/discord/login`, que redireciona para o Discord (state + PKCE num cookie de curta duração).
2. O callback (`/auth/discord/callback`) cria a sessão (30 dias), grava o cookie `discordo_session` e redireciona para `/`.
   Em falha, redireciona para `/?login_error=expired|cancelled|failed`.
3. `POST /auth/logout` invalida a sessão e apaga o cookie.

Nome e avatar trocados no Discordo (`PATCH /users/@me`) prevalecem sobre os do Discord nos próximos logins. Bio, pronomes e cor do banner não vão no `User` (nem em `USER_UPDATE`): o cartão de perfil busca `GET /users/{user}/profile` ao abrir.

## Gateway (tempo real)

WebSocket em `GET /gateway`. A primeira mensagem do cliente é `{"token": "..."}`, ou `{}` para usar o cookie de sessão. Toda mensagem do servidor é `{"t": TIPO, "d": DADOS}`.
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
| `USER_UPDATE` | `User` | alguém com quem você divide servidor, amizade ou DM trocou nome ou avatar (inclusive você) |
| `VOICE_STATE_UPDATE` | `VoiceState` | alguém entrou, saiu (`channel_id` vazio), mutou ou mudou o que transmite |
| `VOICE_READY` / `VOICE_ERROR` / `RTC_OFFER` / `RTC_ANSWER` | ver "Voz e vídeo" | só para a sessão que está na chamada |

### Tipos

```
User         {id, username, display_name, avatar_url}
Snapshot     {id, name, icon_url, banner_url, owner_id, permissions, roles: [Role], channels: [Channel & {permissions}], members: [Member]}
             // só os canais que você pode ver; permissions = suas permissões no servidor e, em cada canal, nele
             // roles vêm por position desc, id; channels por position, id
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
Invite       {code, guild_id, creator_id, max_uses, uses, expires_at}   // max_uses 0 = sem limite; expires_at null = nunca
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
| `PATCH /users/@me` | `{display_name?, avatar_url?, bio?, pronouns?, banner_color?}` (bio até 190, pronomes até 40, cor `0xRRGGBB` ou `0` para o padrão) | `User` |
| `GET /users/{user}/profile` | | `User` + `{bio, pronouns, banner_color, created_at, mutual_guild_ids}`; 404 para quem não divide servidor, amizade nem DM |
| `POST /images` | `multipart/form-data`, parte `file` (PNG, JPEG, GIF ou WebP, até 8 MB) | `{url}` |
| `GET /images/{image}` | | a imagem (pública) |
| `PATCH /guilds/{guild}` | `{name?, icon_url?, banner_url?}` (imagens: `""` ou uma `url` de `POST /images`) | |
| `DELETE /guilds/{guild}` | | só o dono |
| `POST /guilds/{guild}/invites` | `{max_uses?, max_age?}` (segundos; 0 = sem limite) | `{code, ...}` |
| `GET /guilds/{guild}/invites` | | `[Invite]` com `creator: User`, só os não expirados (gerenciar servidor) |
| `POST /invites/{code}` | | `Snapshot` (entra no servidor) |
| `DELETE /invites/{code}` | | revoga (quem criou ou gerenciar servidor) |
| `DELETE /guilds/{guild}/members/@me` | | sair |
| `DELETE /guilds/{guild}/members/{user}` | | expulsar |
| `GET /guilds/{guild}/bans` | | `[{user: User}]` (banir) |
| `PUT` / `DELETE /guilds/{guild}/bans/{user}` | | banir / desbanir |
| `PUT` / `DELETE /guilds/{guild}/members/{user}/roles/{role}` | | dar / tirar cargo |
| `POST /guilds/{guild}/roles` | `{name?, color?, permissions?, position?}` | `Role` |
| `PATCH` / `DELETE /guilds/{guild}/roles/{role}` | idem | `Role` |
| `POST /guilds/{guild}/roles/{role}/move` | `{up}` | troca o cargo com o vizinho de cima ou de baixo |
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
| `POST /dms/{channel}/recipients` | `{recipient_ids}` | adiciona amigos ao grupo (todos ou nenhum) |
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
| `rtc_offer` | `{sdp, tracks: {mid: nome}}` | oferta da conexão de **envio**; `tracks` nomeia cada faixa pelo `mid` do transceiver |
| `rtc_answer` | `{sdp}` | resposta à oferta da conexão de **recepção** |
| `rtc_subscribe` | `{user_id, track, on, quality?}` | começa/para de receber `camera` ou `screen` de alguém; `quality`: `auto` (padrão), `high`, `medium` ou `low` |

Servidor → cliente: `VOICE_READY {channel_id, connection: {kind, url?, token?}}`, `VOICE_ERROR {op, error}`, `RTC_ANSWER {sdp}`, `RTC_OFFER {sdp}`.

O `VOICE_READY` chega antes de qualquer outra mensagem da chamada; `connection.kind` diz como a mídia trafega (abaixo).

### Mídia (`connection.kind == "pion"`)

O cliente mantém **duas** conexões WebRTC com o servidor, para as ofertas nunca colidirem:

- **Envio**: o cliente cria, adiciona suas faixas e manda `rtc_offer` sempre que o que ele envia muda; recebe `RTC_ANSWER`.
- **Recepção**: o servidor manda `RTC_OFFER` sempre que o que o cliente recebe muda; o cliente responde `rtc_answer`.

Sem trickle ICE: espere a coleta de candidatos terminar antes de enviar um SDP (os do servidor já vêm completos).

O **nome** da faixa (em `tracks`, pelo `mid`; sem ele, o ID da faixa) diz o que ela é: `mic`, `screen-audio` (áudio, Opus), `camera`, `screen` (vídeo). Outros nomes são ignorados.
`mic` exige a permissão falar; as demais, transmitir vídeo. O servidor repassa sem transcodificar: quem recebe ganha o codec de quem envia.
Nas ofertas de recepção, cada seção tem `a=msid:<id do usuário dono> <nome da faixa>`; use o `mid` para saber de quem é cada faixa recebida.

Áudio chega de todos automaticamente. Vídeo só depois de `rtc_subscribe`: use `tracks` do `VoiceState` para saber quem está transmitindo o quê.

**Simulcast.** Uma faixa de vídeo pode ir em até três camadas (`a=simulcast` com rids `h` completa, `m` ~720p, `l` ~360p;
o servidor também aceita uma camada só, sem rid). Cada camada precisa levar as extensões RTP `sdes:mid` e `sdes:rtp-stream-id`.
Quem assiste recebe **uma** faixa com uma camada: `high`/`medium`/`low` escolhem `h`/`m`/`l` (se faltar, a mais próxima abaixo,
senão acima); `auto` começa em `h`, desce uma camada quando os relatórios de recepção (RTCP RR) de quem assiste mostram mais de ~10%
de perda e sobe de volta depois de ~10 relatórios seguidos abaixo de ~2%. Para mudar a qualidade, mande `rtc_subscribe` com `on: true`
de novo: não há renegociação. A troca acontece no próximo quadro-chave da nova camada, com números de sequência e timestamps
reescritos para formar um fluxo contínuo. As extensões de cabeçalho de quem envia não são repassadas.

### Mídia (`connection.kind == "livekit"`)

O cliente conecta a um servidor LiveKit em `url` com `token` (válido por 2 minutos, só para entrar; a sala é o ID do canal
e a identidade é o seu ID de usuário). Nada de `rtc_*`: esses ops respondem `VOICE_ERROR`.

A fonte (`source`) de cada faixa no LiveKit é o nome dela aqui: `microphone` = `mic`, `camera` = `camera`,
`screen_share` = `screen`, `screen_share_audio` = `screen-audio`. O token só deixa publicar `microphone` com a permissão
falar e as demais com transmitir vídeo; o servidor sabe o que cada um publica pelos webhooks do LiveKit e repassa em `tracks` do `VoiceState`.

Entre sem assinatura automática e assine áudio de todos e vídeo só de quem você quer ver. `voice_leave` (ou cair o gateway)
tira você da sala do LiveKit; permissões novas valem a partir do próximo `voice_join`.
