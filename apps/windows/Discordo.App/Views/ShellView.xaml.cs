using System.Collections.Specialized;
using System.ComponentModel;
using Discordo.App.ViewModels;
using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;
using Microsoft.UI.Xaml.Input;
using Windows.ApplicationModel.DataTransfer;
using Windows.System;

namespace Discordo.App.Views;

public sealed partial class ShellView : UserControl
{
    public ShellViewModel ViewModel { get; }

    public ShellView(ShellViewModel viewModel)
    {
        ViewModel = viewModel;
        InitializeComponent();
        InviteButton.IsEnabled = Composer.IsEnabled = false; // until something is selected
        ViewModel.PropertyChanged += OnViewModelChanged;
        ViewModel.Messages.CollectionChanged += OnMessagesChanged;
        Unloaded += (_, _) =>
        {
            ViewModel.PropertyChanged -= OnViewModelChanged;
            ViewModel.Messages.CollectionChanged -= OnMessagesChanged;
        };
    }

    // The view model owns the selection (it survives list rebuilds); the lists just mirror it.
    private void OnViewModelChanged(object? sender, PropertyChangedEventArgs e)
    {
        if (e.PropertyName == nameof(ShellViewModel.SelectedGuild))
        {
            GuildList.SelectedItem = ViewModel.SelectedGuild;
            InviteButton.IsEnabled = ViewModel.SelectedGuild is not null;
        }
        else if (e.PropertyName == nameof(ShellViewModel.SelectedChannel))
        {
            ChannelList.SelectedItem = ViewModel.SelectedChannel;
            Composer.IsEnabled = ViewModel.SelectedChannel is not null;
        }
        else if (e.PropertyName == nameof(ShellViewModel.InCall))
        {
            CallBar.Visibility = ViewModel.InCall ? Visibility.Visible : Visibility.Collapsed;
        }
    }

    private void OnGuildSelected(object sender, SelectionChangedEventArgs e) =>
        ViewModel.SelectGuild(GuildList.SelectedItem as GuildItem);

    private void OnChannelSelected(object sender, SelectionChangedEventArgs e) =>
        ViewModel.SelectChannel(ChannelList.SelectedItem as ChannelItem);

    private void OnMessagesChanged(object? sender, NotifyCollectionChangedEventArgs e)
    {
        if (e.Action == NotifyCollectionChangedAction.Add && ViewModel.Messages.Count > 0)
        {
            MessageList.ScrollIntoView(ViewModel.Messages[^1]);
        }
    }

    private async void OnComposerKeyDown(object sender, KeyRoutedEventArgs e)
    {
        if (e.Key != VirtualKey.Enter)
        {
            return;
        }
        e.Handled = true;
        var text = Composer.Text;
        Composer.Text = "";
        if (!await ViewModel.SendAsync(text) && Composer.Text.Length == 0)
        {
            Composer.Text = text; // not sent: give the text back
        }
    }

    private async void OnCreateGuild(object sender, RoutedEventArgs e)
    {
        if (await ViewModel.CreateGuildAsync(NewGuildName.Text))
        {
            NewGuildName.Text = "";
            AddFlyout.Hide();
        }
    }

    private async void OnJoinGuild(object sender, RoutedEventArgs e)
    {
        if (await ViewModel.JoinAsync(InviteCode.Text))
        {
            InviteCode.Text = "";
            AddFlyout.Hide();
        }
    }

    private async void OnInvite(object sender, RoutedEventArgs e)
    {
        if (await ViewModel.CreateInviteAsync() is not { } code)
        {
            return;
        }
        var data = new DataPackage();
        data.SetText(code);
        Clipboard.SetContent(data);
        InviteButton.Content = $"Copiado: {code}";
    }

    private void OnToggleMute(object sender, RoutedEventArgs e) => ViewModel.ToggleMute();

    private void OnToggleDeafen(object sender, RoutedEventArgs e) => ViewModel.ToggleDeafen();

    private void OnLeaveCall(object sender, RoutedEventArgs e) => ViewModel.LeaveCall();

    private async void OnLogout(object sender, RoutedEventArgs e) => await ViewModel.LogoutAsync();
}
