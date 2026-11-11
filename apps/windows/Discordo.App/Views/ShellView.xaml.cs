using System.Collections.Specialized;
using System.ComponentModel;
using System.Runtime.InteropServices;
using Discordo.App.ViewModels;
using Microsoft.UI;
using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;
using Microsoft.UI.Xaml.Input;
using Windows.ApplicationModel.DataTransfer;
using Windows.Graphics.Capture;
using Windows.System;
using WinRT;
using WinRT.Interop;

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
        ViewModel.VideoSized += OnVideoSized;
        ViewModel.WatchEnded += HideVideo;
        Unloaded += (_, _) =>
        {
            ViewModel.VideoSized -= OnVideoSized;
            ViewModel.WatchEnded -= HideVideo;
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
        else if (e.PropertyName == nameof(ShellViewModel.Sharing))
        {
            ShareButton.Content = ViewModel.Sharing ? "Parar transmissão" : "Transmitir tela";
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

    private async void OnToggleShare(object sender, RoutedEventArgs e)
    {
        if (ViewModel.Sharing)
        {
            ViewModel.StopShare();
            return;
        }
        // Windows' own picker: every screen and window, with live previews.
        var picker = new GraphicsCapturePicker();
        InitializeWithWindow.Initialize(picker, Win32Interop.GetWindowFromWindowId(XamlRoot.ContentIslandEnvironment.AppWindowId));
        if (await picker.PickSingleItemAsync() is not { } item)
        {
            return; // cancelled
        }
        static int Picked(ComboBox box) => int.Parse((string)((ComboBoxItem)box.SelectedItem).Tag);
        ViewModel.StartShare(item, Picked(ShareHeight), Picked(ShareFps));
    }

    // ---- watching a screen share ----

    private void OnWatch(object sender, RoutedEventArgs e)
    {
        var swapChain = ViewModel.Watch((string)((Button)sender).Tag);
        if (swapChain == 0)
        {
            return;
        }
        try
        {
            // The panel takes its own reference to the swap chain; ours is released below.
            Marshal.ThrowExceptionForHR(VideoPanel.As<ISwapChainPanelNative>().SetSwapChain(swapChain));
            VideoArea.Visibility = Visibility.Visible;
        }
        finally
        {
            Marshal.Release(swapChain);
        }
    }

    private void OnVideoSized(int width, int height)
    {
        // The Viewbox scales the panel to the space available, keeping the aspect ratio.
        (VideoPanel.Width, VideoPanel.Height) = (width, height);
    }

    private void OnCloseVideo(object sender, RoutedEventArgs e)
    {
        ViewModel.Unwatch();
        HideVideo();
    }

    private void HideVideo()
    {
        VideoArea.Visibility = Visibility.Collapsed;
        VideoPanel.As<ISwapChainPanelNative>().SetSwapChain(0);
    }

    /// <summary>How native code hands a swap chain to a WinUI 3 SwapChainPanel.</summary>
    [ComImport, Guid("63aad0b8-7c24-40ff-85a8-640d944cc325"), InterfaceType(ComInterfaceType.InterfaceIsIUnknown)]
    private interface ISwapChainPanelNative
    {
        [PreserveSig]
        int SetSwapChain(nint swapChain);
    }

    private async void OnLogout(object sender, RoutedEventArgs e) => await ViewModel.LogoutAsync();
}
