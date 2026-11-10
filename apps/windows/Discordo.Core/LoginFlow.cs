using System.Diagnostics;
using System.Net;
using System.Net.Sockets;
using System.Security.Cryptography;
using System.Text;

namespace Discordo.Core;

/// <summary>
/// Sign-in through the system browser (RFC 8252): the app listens on a loopback port, the server
/// redirects the browser there with a one-time code, and the PKCE verifier proves the code is ours.
/// </summary>
public static class LoginFlow
{
    public static async Task<Session> LoginAsync(ApiClient api, CancellationToken ct)
    {
        var verifier = Base64Url(RandomNumberGenerator.GetBytes(32));
        var challenge = Base64Url(SHA256.HashData(Encoding.ASCII.GetBytes(verifier)));

        var port = FreePort();
        using var listener = new HttpListener();
        listener.Prefixes.Add($"http://127.0.0.1:{port}/");
        listener.Start();

        var loginUri = new Uri(api.BaseUri, $"auth/discord/login?port={port}&challenge={challenge}");
        Process.Start(new ProcessStartInfo(loginUri.ToString()) { UseShellExecute = true });

        while (true)
        {
            var context = await listener.GetContextAsync().WaitAsync(ct);
            var code = context.Request.QueryString["code"];
            var page = Encoding.UTF8.GetBytes(
                "<!doctype html><meta charset=utf-8><title>Discordo</title>" +
                "<body style='font-family:Segoe UI,sans-serif;text-align:center;margin-top:20vh'>" +
                (code is null ? "<h2>Nada por aqui.</h2>" : "<h2>Tudo certo.</h2><p>Pode fechar esta aba e voltar ao Discordo.</p>"));
            context.Response.StatusCode = code is null ? 404 : 200;
            context.Response.ContentType = "text/html; charset=utf-8";
            await context.Response.OutputStream.WriteAsync(page, ct);
            context.Response.Close();
            if (code is not null) // browsers also ask for /favicon.ico and the like
            {
                return await api.ExchangeAsync(code, verifier, ct);
            }
        }
    }

    private static int FreePort()
    {
        var probe = new TcpListener(IPAddress.Loopback, 0);
        probe.Start();
        var port = ((IPEndPoint)probe.LocalEndpoint).Port;
        probe.Stop();
        return port;
    }

    private static string Base64Url(byte[] bytes) =>
        Convert.ToBase64String(bytes).TrimEnd('=').Replace('+', '-').Replace('/', '_');
}
