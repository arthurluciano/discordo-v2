using CommunityToolkit.Mvvm.ComponentModel;
using Discordo.Core;

namespace Discordo.App.ViewModels;

// What the lists show. Plain classes (reference identity) so list selection survives data refreshes.

public sealed class GuildItem(GuildSnapshot guild) : ObservableObject
{
    private string name = guild.Name;

    public string Id { get; } = guild.Id;

    public string Name
    {
        get => name;
        set
        {
            if (SetProperty(ref name, value))
            {
                OnPropertyChanged(nameof(Initial));
            }
        }
    }

    public string Initial => name.Length == 0 ? "?" : char.ToUpper(name[0]).ToString();
}

public sealed class ChannelItem(Channel channel) : ObservableObject
{
    private string inCall = "";

    public string Id { get; } = channel.Id;
    public string Name { get; } = channel.Name;
    public bool IsText { get; } = channel.Type == Channel.Text;
    public string Glyph => IsText ? "#" : "🔊";

    /// <summary>Names of the people in this voice channel, one per line; empty when nobody is.</summary>
    public string InCall
    {
        get => inCall;
        set
        {
            if (SetProperty(ref inCall, value))
            {
                OnPropertyChanged(nameof(InCallVisibility));
            }
        }
    }

    public Microsoft.UI.Xaml.Visibility InCallVisibility =>
        inCall.Length > 0 ? Microsoft.UI.Xaml.Visibility.Visible : Microsoft.UI.Xaml.Visibility.Collapsed;
}

public sealed class MemberItem(Member member) : ObservableObject
{
    private bool online = member.Online;

    public string Id { get; } = member.User.Id;
    public string Name { get; } = member.Nickname.Length > 0 ? member.Nickname : member.User.DisplayName;

    public bool Online
    {
        get => online;
        set
        {
            if (SetProperty(ref online, value))
            {
                OnPropertyChanged(nameof(Opacity));
                OnPropertyChanged(nameof(Status));
            }
        }
    }

    public double Opacity => online ? 1.0 : 0.45;
    public string Status => online ? "online" : "offline"; // for screen readers; color alone is not enough
}

public sealed class MessageItem(Message message) : ObservableObject
{
    private string content = message.Content;
    private bool edited = message.EditedAt is not null;

    public string Id { get; } = message.Id;
    public string Author { get; } = message.Author.DisplayName;
    public string Time { get; } = message.CreatedAt.ToLocalTime().ToString("g");

    public string Content
    {
        get => content;
        private set => SetProperty(ref content, value);
    }

    public string EditedMark => edited ? "(editada)" : "";

    public void Update(Message updated)
    {
        Content = updated.Content;
        edited = updated.EditedAt is not null;
        OnPropertyChanged(nameof(EditedMark));
    }
}
