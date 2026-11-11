using System.Collections.ObjectModel;
using System.Text.Json;
using CommunityToolkit.Mvvm.ComponentModel;
using Discordo.App.Services;
using Discordo.Core;
using Microsoft.UI.Dispatching;

namespace Discordo.App.ViewModels;

/// <summary>
/// Client state for the main screen. The server pushes whole-guild snapshots, so this keeps the
/// latest snapshot per guild and re-derives the visible lists from it; only messages, members
/// and presence are patched in place.
/// </summary>
public sealed class ShellViewModel(ApiClient api) : ObservableObject
{
    private readonly DispatcherQueue ui = DispatcherQueue.GetForCurrentThread();
    private readonly Dictionary<string, GuildSnapshot> snapshots = [];
    private readonly Dictionary<string, VoiceState> voiceStates = []; // by user ID
    private string myId = "";
    private MediaCall? call;
    private string? callChannelId;
    private bool muted, deafened, sharing;
    private string? watching; // user ID whose screen we are showing

    /// <summary>People in our call who are sharing their screen.</summary>
    public ObservableCollection<StreamItem> Streams { get; } = [];

    /// <summary>The watched video has this picture size (raised on the UI thread).</summary>
    public event Action<int, int>? VideoSized;
    /// <summary>The video being watched is over; the view should hide it.</summary>
    public event Action? WatchEnded;

    /// <summary>Sends a client message over the gateway; set by whoever owns the connection.</summary>
    public Func<string, object, Task> SendGateway { get; set; } = (_, _) => Task.CompletedTask;

    private GuildItem? selectedGuild;
    private ChannelItem? selectedChannel;
    private string status = "Conectando…";

    public ObservableCollection<GuildItem> Guilds { get; } = [];
    public ObservableCollection<ChannelItem> Channels { get; } = [];
    public ObservableCollection<MemberItem> Members { get; } = [];
    public ObservableCollection<MessageItem> Messages { get; } = [];

    public event Action? LoggedOut;

    public GuildItem? SelectedGuild
    {
        get => selectedGuild;
        private set
        {
            if (SetProperty(ref selectedGuild, value))
            {
                OnPropertyChanged(nameof(GuildName));
            }
        }
    }

    public ChannelItem? SelectedChannel
    {
        get => selectedChannel;
        private set
        {
            if (SetProperty(ref selectedChannel, value))
            {
                OnPropertyChanged(nameof(ChannelTitle));
            }
        }
    }

    public string GuildName => selectedGuild?.Name ?? "Sem servidores";
    public string ChannelTitle => selectedChannel is null ? "" : $"{selectedChannel.Glyph} {selectedChannel.Name}";

    /// <summary>Connection trouble or the last error; empty when all is well.</summary>
    public string Status
    {
        get => status;
        private set => SetProperty(ref status, value);
    }

    // ---- gateway (called on a background thread) ----

    public void OnGatewayEvent(string type, JsonElement data) => ui.TryEnqueue(() =>
    {
        try
        {
            Apply(type, data);
        }
        catch (Exception e) when (e is JsonException or KeyNotFoundException or InvalidOperationException)
        {
            Status = $"Evento {type} inesperado: {e.Message}"; // a newer server; the next READY resyncs everything
        }
    });

    public void OnConnectionChanged(bool connected) => ui.TryEnqueue(() => Status = connected ? "" : "Reconectando…");

    private void Apply(string type, JsonElement d)
    {
        switch (type)
        {
            case "READY":
            {
                var ready = Read<Ready>(d);
                myId = ready.User.Id;
                voiceStates.Clear();
                foreach (var s in ready.VoiceStates ?? [])
                {
                    voiceStates[s.UserId] = s;
                }
                // A new connection means the server already dropped our call; go back in if we were in one.
                var rejoin = callChannelId;
                EndCall();
                if (rejoin is not null)
                {
                    _ = SendGateway("voice_join", new { channel_id = rejoin });
                }
                var keep = selectedGuild?.Id;
                snapshots.Clear();
                Guilds.Clear();
                foreach (var g in ready.Guilds ?? [])
                {
                    snapshots[g.Id] = g;
                    Guilds.Add(new GuildItem(g));
                }
                SelectedGuild = Guilds.FirstOrDefault(g => g.Id == keep) ?? Guilds.FirstOrDefault();
                ShowGuild(reloadMessages: true); // messages may have been missed while disconnected
                break;
            }
            case "GUILD_SYNC":
            {
                var g = Read<GuildSnapshot>(d);
                snapshots[g.Id] = g;
                var item = Guilds.FirstOrDefault(i => i.Id == g.Id);
                if (item is null)
                {
                    Guilds.Add(item = new GuildItem(g));
                    SelectedGuild ??= item;
                }
                item.Name = g.Name;
                if (selectedGuild?.Id == g.Id)
                {
                    OnPropertyChanged(nameof(GuildName));
                    ShowGuild(reloadMessages: false);
                }
                break;
            }
            case "GUILD_DELETE":
            {
                var id = d.GetProperty("id").GetString();
                if (id is null || !snapshots.Remove(id))
                {
                    break;
                }
                if (Guilds.FirstOrDefault(i => i.Id == id) is { } item)
                {
                    Guilds.Remove(item);
                }
                if (selectedGuild?.Id == id)
                {
                    SelectedGuild = Guilds.FirstOrDefault();
                    ShowGuild(reloadMessages: false);
                }
                break;
            }
            case "MEMBER_ADD":
            {
                var guildId = d.GetProperty("guild_id").GetString() ?? "";
                var member = Read<Member>(d.GetProperty("member"));
                if (!snapshots.TryGetValue(guildId, out var g) || g.Members.Any(m => m.User.Id == member.User.Id))
                {
                    break;
                }
                g.Members.Add(member);
                if (selectedGuild?.Id == guildId)
                {
                    Members.Add(new MemberItem(member));
                }
                break;
            }
            case "MEMBER_REMOVE":
            {
                var guildId = d.GetProperty("guild_id").GetString() ?? "";
                var userId = d.GetProperty("user_id").GetString();
                if (!snapshots.TryGetValue(guildId, out var g))
                {
                    break;
                }
                g.Members.RemoveAll(m => m.User.Id == userId);
                if (selectedGuild?.Id == guildId && Members.FirstOrDefault(m => m.Id == userId) is { } item)
                {
                    Members.Remove(item);
                }
                break;
            }
            case "PRESENCE_UPDATE":
            {
                var userId = d.GetProperty("user_id").GetString();
                var online = d.GetProperty("online").GetBoolean();
                foreach (var g in snapshots.Values)
                {
                    var i = g.Members.FindIndex(m => m.User.Id == userId);
                    if (i >= 0)
                    {
                        g.Members[i] = g.Members[i] with { Online = online };
                    }
                }
                if (Members.FirstOrDefault(m => m.Id == userId) is { } item)
                {
                    item.Online = online;
                }
                break;
            }
            case "MESSAGE_CREATE":
            {
                var m = Read<Message>(d);
                if (m.ChannelId == selectedChannel?.Id && Messages.All(i => i.Id != m.Id))
                {
                    Messages.Add(new MessageItem(m));
                }
                break;
            }
            case "MESSAGE_UPDATE":
            {
                var m = Read<Message>(d);
                Messages.FirstOrDefault(i => i.Id == m.Id)?.Update(m);
                break;
            }
            case "MESSAGE_DELETE":
            {
                var id = d.GetProperty("id").GetString();
                if (Messages.FirstOrDefault(i => i.Id == id) is { } item)
                {
                    Messages.Remove(item);
                }
                break;
            }
            case "VOICE_STATE_UPDATE":
            {
                var s = Read<VoiceState>(d);
                if (s.ChannelId.Length == 0)
                {
                    voiceStates.Remove(s.UserId);
                }
                else
                {
                    voiceStates[s.UserId] = s;
                }
                RefreshVoice();
                break;
            }
            case "VOICE_READY":
            {
                EndCall();
                callChannelId = d.GetProperty("channel_id").GetString();
                call = MediaCall.Start();
                if (call is null)
                {
                    Status = "Não foi possível iniciar o áudio.";
                    LeaveCall();
                    break;
                }
                call.Signal += (op, sdp) => _ = SendGateway(op, new { sdp });
                call.Closed += () => ui.TryEnqueue(() =>
                {
                    Status = "A chamada caiu.";
                    LeaveCall();
                });
                call.VideoSized += (width, height) => ui.TryEnqueue(() => VideoSized?.Invoke(width, height));
                call.ShareStopped += () => ui.TryEnqueue(() =>
                {
                    Status = "A transmissão parou: falha na captura ou no encoder.";
                    StopShare();
                });
                if (!call.EnableMic())
                {
                    Status = "Sem microfone: você está só ouvindo.";
                }
                OnPropertyChanged(nameof(CallTitle));
                OnPropertyChanged(nameof(InCall));
                RefreshVoice();
                break;
            }
            case "RTC_OFFER" or "RTC_ANSWER":
                call?.HandleSignal(type, d.GetProperty("sdp").GetString() ?? "");
                break;
            case "VOICE_ERROR":
                Status = "Chamada: " + d.GetProperty("error").GetString();
                break;
            // DMs, relationships, reactions and typing arrive too; their screens come in a later milestone.
        }
    }

    private static T Read<T>(JsonElement e) =>
        e.Deserialize<T>(ApiClient.Json) ?? throw new JsonException($"null {typeof(T).Name}");

    // ---- selection ----

    public void SelectGuild(GuildItem? guild)
    {
        if (guild is null || guild.Id == selectedGuild?.Id)
        {
            return;
        }
        SelectedGuild = guild;
        ShowGuild(reloadMessages: false);
    }

    public void SelectChannel(ChannelItem? channel)
    {
        if (channel is null || channel.Id == selectedChannel?.Id)
        {
            return;
        }
        SelectedChannel = channel;
        _ = LoadMessagesAsync();
        if (!channel.IsText && channel.Id != callChannelId)
        {
            _ = SendGateway("voice_join", new { channel_id = channel.Id }); // answered with VOICE_READY or VOICE_ERROR
        }
    }

    // ---- call ----

    public bool InCall => call is not null;

    public string CallTitle =>
        call is null ? "" : "Em chamada" + (muted ? " · mudo" : "") + (deafened ? " · sem som" : "") + (sharing ? " · transmitindo" : "");

    public bool Sharing => sharing;

    /// <summary>Starts sharing the primary screen at up to <paramref name="height"/> lines.</summary>
    public void StartShare(int height, int fps)
    {
        if (call is null)
        {
            return;
        }
        var result = call.ShareScreen(height, fps);
        sharing = result != 0;
        Status = result switch
        {
            0 => "Não foi possível transmitir a tela.",
            2 => "Transmitindo com codificação por CPU: nenhum encoder de GPU disponível.",
            _ => "",
        };
        OnPropertyChanged(nameof(Sharing));
        OnPropertyChanged(nameof(CallTitle));
    }

    public void StopShare()
    {
        call?.StopShare();
        sharing = false;
        OnPropertyChanged(nameof(Sharing));
        OnPropertyChanged(nameof(CallTitle));
    }

    /// <summary>
    /// Starts watching someone's screen. Returns the swap chain (IDXGISwapChain1*) to show, or 0 on failure;
    /// the caller owns one reference to it.
    /// </summary>
    public nint Watch(string userId)
    {
        if (call is null)
        {
            return 0;
        }
        Unwatch();
        var swapChain = call.Watch(userId, "screen");
        if (swapChain == 0)
        {
            Status = "Não foi possível abrir o vídeo.";
            return 0;
        }
        watching = userId;
        _ = SendGateway("rtc_subscribe", new { user_id = userId, track = "screen", on = true });
        return swapChain;
    }

    public void Unwatch()
    {
        if (watching is not { } userId)
        {
            return;
        }
        watching = null;
        call?.Unwatch(userId, "screen");
        _ = SendGateway("rtc_subscribe", new { user_id = userId, track = "screen", on = false });
    }

    public void ToggleMute() => SetCallFlags(!muted, deafened);

    public void ToggleDeafen() => SetCallFlags(muted, !deafened);

    private void SetCallFlags(bool mute, bool deafen)
    {
        if (call is null)
        {
            return;
        }
        (muted, deafened) = (mute || deafen, deafen); // deafened implies muted, as on the server
        call.SetMuted(muted);
        call.SetDeafened(deafened);
        _ = SendGateway("voice_state", new { muted, deafened });
        OnPropertyChanged(nameof(CallTitle));
    }

    public void LeaveCall()
    {
        if (callChannelId is not null)
        {
            _ = SendGateway("voice_leave", new { });
        }
        EndCall();
    }

    /// <summary>Tears down the local side of the call.</summary>
    private void EndCall()
    {
        if (watching is not null)
        {
            watching = null; // the call is going away, and the server-side subscription with it
            WatchEnded?.Invoke();
        }
        call?.Dispose();
        call = null;
        callChannelId = null;
        (muted, deafened, sharing) = (false, false, false);
        Streams.Clear();
        OnPropertyChanged(nameof(Sharing));
        OnPropertyChanged(nameof(CallTitle));
        OnPropertyChanged(nameof(InCall));
    }

    /// <summary>Shows who is in each voice channel of the selected guild.</summary>
    private void RefreshVoice()
    {
        var names = selectedGuild is not null && snapshots.TryGetValue(selectedGuild.Id, out var g)
            ? g.Members.ToDictionary(m => m.User.Id, m => m.Nickname.Length > 0 ? m.Nickname : m.User.DisplayName)
            : new Dictionary<string, string>();
        foreach (var channel in Channels)
        {
            channel.InCall = string.Join("\n", voiceStates.Values
                .Where(s => s.ChannelId == channel.Id)
                .Select(s => (names.GetValueOrDefault(s.UserId) ?? "?") + (s.Muted ? " (mudo)" : ""))
                .Order());
        }

        // Who in our call is sharing their screen.
        var live = voiceStates.Values
            .Where(s => callChannelId is not null && s.ChannelId == callChannelId && s.UserId != myId && s.Tracks.Contains("screen"))
            .Select(s => new StreamItem(s.UserId, names.GetValueOrDefault(s.UserId) ?? "?"))
            .OrderBy(s => s.Name)
            .ToList();
        if (!live.SequenceEqual(Streams))
        {
            Streams.Clear();
            foreach (var stream in live)
            {
                Streams.Add(stream);
            }
        }
        if (watching is not null && live.All(s => s.UserId != watching))
        {
            Unwatch(); // they stopped sharing or left
            WatchEnded?.Invoke();
        }
    }

    /// <summary>Rebuilds the channel and member lists from the selected guild's snapshot, keeping the channel when it still exists.</summary>
    private void ShowGuild(bool reloadMessages)
    {
        var previous = selectedChannel?.Id;
        Channels.Clear();
        Members.Clear();
        if (selectedGuild is not null && snapshots.TryGetValue(selectedGuild.Id, out var g))
        {
            foreach (var c in g.Channels.OrderBy(c => c.Position).ThenBy(c => c.Id))
            {
                Channels.Add(new ChannelItem(c));
            }
            foreach (var m in g.Members)
            {
                Members.Add(new MemberItem(m));
            }
        }
        RefreshVoice();
        // Always a fresh instance, so the view re-selects it in the rebuilt list.
        SelectedChannel = Channels.FirstOrDefault(c => c.Id == previous) ?? Channels.FirstOrDefault(c => c.IsText);
        if (reloadMessages || selectedChannel?.Id != previous)
        {
            _ = LoadMessagesAsync();
        }
    }

    private async Task LoadMessagesAsync()
    {
        Messages.Clear();
        if (selectedChannel is not { } channel)
        {
            return;
        }
        try
        {
            var newestFirst = await api.ListMessagesAsync(channel.Id);
            if (selectedChannel?.Id != channel.Id)
            {
                return; // the user moved on while this was loading
            }
            Messages.Clear();
            foreach (var m in Enumerable.Reverse(newestFirst))
            {
                Messages.Add(new MessageItem(m));
            }
        }
        catch (Exception e) when (e is ApiException or HttpRequestException or TaskCanceledException)
        {
            Status = "Não foi possível carregar as mensagens: " + e.Message;
        }
    }

    // ---- actions (called by the view; failures land in Status) ----

    /// <summary>Returns false when the message was not sent, so the view keeps the text.</summary>
    public Task<bool> SendAsync(string text) =>
        selectedChannel is { } channel && !string.IsNullOrWhiteSpace(text)
            ? TryAsync(() => api.SendMessageAsync(channel.Id, text)) // the message itself arrives as MESSAGE_CREATE
            : Task.FromResult(false);

    public Task<bool> CreateGuildAsync(string name) => TryAsync(() => api.CreateGuildAsync(name));

    public Task<bool> JoinAsync(string inviteCode) => TryAsync(() => api.JoinAsync(inviteCode.Trim()));

    /// <summary>Returns the invite code, or null on failure.</summary>
    public async Task<string?> CreateInviteAsync()
    {
        string? code = null;
        if (selectedGuild is { } guild)
        {
            await TryAsync(async () => code = (await api.CreateInviteAsync(guild.Id)).Code);
        }
        return code;
    }

    public async Task LogoutAsync()
    {
        LeaveCall();
        await TryAsync(() => api.LogoutAsync()); // signing out locally must work even if the server is unreachable
        LoggedOut?.Invoke();
    }

    private async Task<bool> TryAsync(Func<Task> action)
    {
        try
        {
            await action();
            Status = "";
            return true;
        }
        catch (Exception e) when (e is ApiException or HttpRequestException or TaskCanceledException)
        {
            Status = e.Message;
            return false;
        }
    }
}
