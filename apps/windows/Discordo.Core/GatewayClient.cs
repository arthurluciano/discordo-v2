using System.Net.WebSockets;
using System.Text.Json;

namespace Discordo.Core;

/// <summary>
/// Realtime connection. Reconnects forever with backoff; every (re)connection starts with a READY
/// event carrying the full state, so callers never need to patch over a gap.
/// Events are raised on a background thread.
/// </summary>
public sealed class GatewayClient
{
    public event Action<string, JsonElement>? Event;
    public event Action<bool>? ConnectionChanged;
    /// <summary>The server rejected the token; the client stops reconnecting.</summary>
    public event Action? Unauthorized;

    public async Task RunAsync(Uri gatewayUri, string token, CancellationToken ct)
    {
        var delay = TimeSpan.FromSeconds(1);
        while (!ct.IsCancellationRequested)
        {
            try
            {
                using var socket = new ClientWebSocket();
                await socket.ConnectAsync(gatewayUri, ct);
                var hello = new ArraySegment<byte>(JsonSerializer.SerializeToUtf8Bytes(new { token }));
                await socket.SendAsync(hello, WebSocketMessageType.Text, true, ct);
                ConnectionChanged?.Invoke(true);
                delay = TimeSpan.FromSeconds(1);

                await ReceiveAsync(socket, ct);
                if (socket.CloseStatus == WebSocketCloseStatus.PolicyViolation)
                {
                    Unauthorized?.Invoke();
                    return;
                }
            }
            catch (OperationCanceledException) when (ct.IsCancellationRequested)
            {
                return;
            }
            catch (Exception e) when (e is WebSocketException or IOException or JsonException or OperationCanceledException)
            {
                // Network trouble: fall through to reconnect.
            }
            ConnectionChanged?.Invoke(false);
            try
            {
                await Task.Delay(delay, ct);
            }
            catch (OperationCanceledException)
            {
                return;
            }
            delay = TimeSpan.FromSeconds(Math.Min(delay.TotalSeconds * 2, 30));
        }
    }

    private async Task ReceiveAsync(ClientWebSocket socket, CancellationToken ct)
    {
        var buffer = new ArraySegment<byte>(new byte[16 * 1024]);
        using var message = new MemoryStream();
        while (socket.State == WebSocketState.Open)
        {
            var result = await socket.ReceiveAsync(buffer, ct);
            if (result.MessageType == WebSocketMessageType.Close)
            {
                return;
            }
            message.Write(buffer.Array!, 0, result.Count);
            if (!result.EndOfMessage)
            {
                continue;
            }
            using var doc = JsonDocument.Parse(message.GetBuffer().AsMemory(0, (int)message.Length));
            message.SetLength(0);
            var type = doc.RootElement.GetProperty("t").GetString() ?? "";
            Event?.Invoke(type, doc.RootElement.GetProperty("d").Clone()); // Clone: it outlives the document
        }
    }
}
