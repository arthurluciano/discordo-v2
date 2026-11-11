using Discordo.App.Services;
using Discordo.App.ViewModels;
using Discordo.App.Views;
using Discordo.Core;
using Microsoft.UI.Xaml;

namespace Discordo.App;

/// <summary>Owns the session: shows the login or the shell, and runs the gateway while signed in.</summary>
public sealed partial class MainWindow : Window
{
    private ShellViewModel? shell;
    // DISCORDO_SERVER=http://localhost:8080 points it at a local server.
    private readonly ApiClient api = new(new Uri(Environment.GetEnvironmentVariable("DISCORDO_SERVER") ?? "https://discordo.arluck.com.br"));
    private CancellationTokenSource? session;

    public MainWindow()
    {
        InitializeComponent();
        Title = "Discordo";
        ExtendsContentIntoTitleBar = true;
        SetTitleBar(TitleBar);
        Closed += (_, _) =>
        {
            shell?.LeaveCall();
            session?.Cancel();
        };

        if (TokenStore.Load() is { } token)
        {
            ShowShell(token); // if the token is no longer valid, the gateway says so and we fall back to the login
        }
        else
        {
            ShowLogin();
        }
    }

    private void ShowLogin()
    {
        var login = new LoginView(api);
        login.LoggedIn += s =>
        {
            TokenStore.Save(s.Token);
            ShowShell(s.Token);
            Activate(); // come back to the front after the browser
        };
        Host.Content = login;
    }

    private void ShowShell(string token)
    {
        api.Token = token;
        session = new CancellationTokenSource();
        var viewModel = new ShellViewModel(api);
        viewModel.LoggedOut += SignOut;
        shell = viewModel;

        var gateway = new GatewayClient();
        viewModel.SendGateway = gateway.SendAsync;
        gateway.Event += viewModel.OnGatewayEvent;
        gateway.ConnectionChanged += viewModel.OnConnectionChanged;
        gateway.Unauthorized += () => DispatcherQueue.TryEnqueue(SignOut);
        _ = gateway.RunAsync(api.GatewayUri, token, session.Token);

        Host.Content = new ShellView(viewModel);
    }

    private void SignOut()
    {
        session?.Cancel();
        session = null;
        TokenStore.Clear();
        api.Token = null;
        ShowLogin();
    }
}
