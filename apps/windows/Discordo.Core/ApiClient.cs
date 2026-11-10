using System.Net;
using System.Net.Http.Headers;
using System.Net.Http.Json;
using System.Text.Json;

namespace Discordo.Core;

public sealed class ApiException(HttpStatusCode status, string message) : Exception(message)
{
    public HttpStatusCode Status { get; } = status;
}

/// <summary>REST client for the Discordo server.</summary>
public sealed class ApiClient(Uri baseUri)
{
    public static readonly JsonSerializerOptions Json = new() { PropertyNamingPolicy = JsonNamingPolicy.SnakeCaseLower };

    private readonly HttpClient http = new() { BaseAddress = baseUri, Timeout = TimeSpan.FromSeconds(30) };

    public Uri BaseUri { get; } = baseUri;

    public Uri GatewayUri => new UriBuilder(new Uri(BaseUri, "gateway")) { Scheme = BaseUri.Scheme == "https" ? "wss" : "ws" }.Uri;

    public string? Token
    {
        set => http.DefaultRequestHeaders.Authorization = value is null ? null : new AuthenticationHeaderValue("Bearer", value);
    }

    public Task<Session> ExchangeAsync(string code, string verifier, CancellationToken ct = default) =>
        SendAsync<Session>(HttpMethod.Post, "auth/token", new { code, verifier }, ct);

    public Task LogoutAsync(CancellationToken ct = default) => SendAsync(HttpMethod.Post, "auth/logout", null, ct);

    public Task<GuildSnapshot> CreateGuildAsync(string name, CancellationToken ct = default) =>
        SendAsync<GuildSnapshot>(HttpMethod.Post, "guilds", new { name }, ct);

    public Task<Invite> CreateInviteAsync(string guildId, CancellationToken ct = default) =>
        SendAsync<Invite>(HttpMethod.Post, $"guilds/{guildId}/invites", new { }, ct);

    public Task<GuildSnapshot> JoinAsync(string inviteCode, CancellationToken ct = default) =>
        SendAsync<GuildSnapshot>(HttpMethod.Post, $"invites/{Uri.EscapeDataString(inviteCode)}", null, ct);

    /// <summary>Newest first.</summary>
    public Task<List<Message>> ListMessagesAsync(string channelId, string? before = null, CancellationToken ct = default) =>
        SendAsync<List<Message>>(HttpMethod.Get, $"channels/{channelId}/messages" + (before is null ? "" : $"?before={before}"), null, ct);

    public Task<Message> SendMessageAsync(string channelId, string content, CancellationToken ct = default) =>
        SendAsync<Message>(HttpMethod.Post, $"channels/{channelId}/messages", new { content }, ct);

    private async Task<T> SendAsync<T>(HttpMethod method, string path, object? body, CancellationToken ct)
    {
        using var response = await RequestAsync(method, path, body, ct);
        return await response.Content.ReadFromJsonAsync<T>(Json, ct)
            ?? throw new ApiException(response.StatusCode, "empty response");
    }

    private async Task SendAsync(HttpMethod method, string path, object? body, CancellationToken ct)
    {
        using var response = await RequestAsync(method, path, body, ct);
    }

    private async Task<HttpResponseMessage> RequestAsync(HttpMethod method, string path, object? body, CancellationToken ct)
    {
        using var request = new HttpRequestMessage(method, path);
        if (body is not null)
        {
            request.Content = JsonContent.Create(body, options: Json);
        }
        var response = await http.SendAsync(request, ct);
        if (response.IsSuccessStatusCode)
        {
            return response;
        }
        using (response)
        {
            var message = response.ReasonPhrase ?? "request failed";
            try
            {
                var error = await response.Content.ReadFromJsonAsync<JsonElement>(Json, ct);
                message = error.GetProperty("error").GetString() ?? message;
            }
            catch (Exception e) when (e is JsonException or KeyNotFoundException or InvalidOperationException)
            {
                // Not our JSON error shape (a proxy page, for example): keep the status text.
            }
            throw new ApiException(response.StatusCode, message);
        }
    }
}
