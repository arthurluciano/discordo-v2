namespace Discordo.Core;

// Wire types, mirroring docs/protocol.md. JSON names are snake_case (see ApiClient.Json).

public record User(string Id, string Username, string DisplayName, string AvatarUrl);

public record Role(string Id, string GuildId, string Name, int Color, long Permissions, int Position);

public record Member(User User, string Nickname, List<string> Roles, DateTimeOffset JoinedAt, bool Online);

public record Overwrite(string TargetId, bool IsMember, long Allow, long Deny);

public record Channel(string Id, string GuildId, int Type, string Name, string Topic, int Position, List<Overwrite> Overwrites)
{
    public const int Text = 0, Voice = 1;
}

public record GuildSnapshot(
    string Id, string Name, string IconUrl, string OwnerId, long Permissions,
    List<Role> Roles, List<Channel> Channels, List<Member> Members);

public record Attachment(string Id, string Filename, string ContentType, long Size);

public record Reaction(string Emoji, List<string> Users);

public record Message(
    string Id, string ChannelId, User Author, string Content, DateTimeOffset CreatedAt, DateTimeOffset? EditedAt,
    List<string> Mentions, bool MentionEveryone, List<Attachment> Attachments, List<Reaction> Reactions);

public record DmChannel(string Id, int Type, string Name, List<User> Recipients);

public record Relationship(User User, int Status, bool Online);

/// <summary>Someone's presence in a call. An empty ChannelId means they left.</summary>
public record VoiceState(string UserId, string ChannelId, string? GuildId, bool Muted, bool Deafened, List<string> Tracks);

public record Ready(
    User User, List<GuildSnapshot> Guilds, List<DmChannel> Dms, List<Relationship> Relationships, List<VoiceState> VoiceStates);

public record Invite(string Code, string GuildId);

public record Session(string Token, User User);
