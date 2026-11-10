using Discordo.Core;
using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;

namespace Discordo.App.Views;

public sealed partial class LoginView : UserControl
{
    private readonly ApiClient api;
    private CancellationTokenSource? attempt;

    public event Action<Session>? LoggedIn;

    public LoginView(ApiClient api)
    {
        this.api = api;
        InitializeComponent();
        Unloaded += (_, _) => attempt?.Cancel();
    }

    private async void OnLoginClick(object sender, RoutedEventArgs e)
    {
        attempt = new CancellationTokenSource(TimeSpan.FromMinutes(5));
        SetBusy(true, "Continue no navegador…");
        try
        {
            var session = await LoginFlow.LoginAsync(api, attempt.Token);
            LoggedIn?.Invoke(session);
        }
        catch (OperationCanceledException)
        {
            SetBusy(false, "");
        }
        catch (Exception ex) // network, server down, rejected code: all end the attempt the same way
        {
            SetBusy(false, "Não foi possível entrar: " + ex.Message);
        }
    }

    private void OnCancelClick(object sender, RoutedEventArgs e) => attempt?.Cancel();

    private void SetBusy(bool busy, string status)
    {
        LoginButton.IsEnabled = !busy;
        CancelButton.Visibility = busy ? Visibility.Visible : Visibility.Collapsed;
        Status.Text = status;
    }
}
