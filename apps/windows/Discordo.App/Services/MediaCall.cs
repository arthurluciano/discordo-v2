using System.Collections.Concurrent;
using System.Runtime.InteropServices;
using System.Text.Json;

namespace Discordo.App.Services;

/// <summary>
/// A call in the native media engine (media_core.dll, built from crates/media-core). It owns the
/// microphone, the speakers and the WebRTC connections; this class only relays signaling.
/// Events are raised on a background thread.
/// </summary>
public sealed class MediaCall : IDisposable
{
    /// <summary>Send this over the gateway: (op, sdp).</summary>
    public event Action<string, string>? Signal;
    /// <summary>The media connection was lost.</summary>
    public event Action? Closed;

    private static readonly ConcurrentDictionary<nint, MediaCall> Calls = new();
    private static readonly EventCallback Callback = OnEvent; // static: the native side may call it at any time
    private static nint nextId;

    private readonly nint id = Interlocked.Increment(ref nextId);
    private nint handle;

    private MediaCall()
    {
    }

    /// <summary>Returns null when the audio output or the network could not be set up.</summary>
    public static MediaCall? Start()
    {
        var call = new MediaCall();
        Calls[call.id] = call; // registered first: events can fire before dm_call_start returns
        try
        {
            call.handle = dm_call_start(Callback, call.id);
        }
        catch (Exception e) when (e is DllNotFoundException or EntryPointNotFoundException or BadImageFormatException)
        {
            call.handle = 0;
        }
        if (call.handle == 0)
        {
            Calls.TryRemove(call.id, out _);
            return null;
        }
        return call;
    }

    /// <summary>Feeds RTC_OFFER / RTC_ANSWER from the gateway.</summary>
    public void HandleSignal(string type, string sdp)
    {
        if (handle != 0)
        {
            dm_call_signal(handle, type, sdp);
        }
    }

    /// <summary>False when there is no usable microphone.</summary>
    public bool EnableMic() => handle != 0 && dm_call_enable_mic(handle);

    public void SetMuted(bool muted)
    {
        if (handle != 0)
        {
            dm_call_set_muted(handle, muted);
        }
    }

    public void SetDeafened(bool deafened)
    {
        if (handle != 0)
        {
            dm_call_set_deafened(handle, deafened);
        }
    }

    public void Dispose()
    {
        var h = Interlocked.Exchange(ref handle, 0);
        if (h != 0)
        {
            Calls.TryRemove(id, out _); // late events from the native side now find nobody
            dm_call_stop(h);
        }
    }

    private static void OnEvent(nint context, nint json)
    {
        try
        {
            if (!Calls.TryGetValue(context, out var call) || Marshal.PtrToStringUTF8(json) is not { } text)
            {
                return;
            }
            using var doc = JsonDocument.Parse(text);
            var root = doc.RootElement;
            switch (root.GetProperty("type").GetString())
            {
                case "signal":
                    call.Signal?.Invoke(root.GetProperty("op").GetString()!, root.GetProperty("sdp").GetString()!);
                    break;
                case "closed":
                    call.Closed?.Invoke();
                    break;
            }
        }
        catch (Exception)
        {
            // An exception must never unwind into native code.
        }
    }

    [UnmanagedFunctionPointer(CallingConvention.Cdecl)]
    private delegate void EventCallback(nint context, nint json);

    private const string Dll = "media_core";

    [DllImport(Dll, CallingConvention = CallingConvention.Cdecl)]
    private static extern nint dm_call_start(EventCallback callback, nint context);

    [DllImport(Dll, CallingConvention = CallingConvention.Cdecl)]
    private static extern void dm_call_signal(nint call, [MarshalAs(UnmanagedType.LPUTF8Str)] string type, [MarshalAs(UnmanagedType.LPUTF8Str)] string sdp);

    [DllImport(Dll, CallingConvention = CallingConvention.Cdecl)]
    [return: MarshalAs(UnmanagedType.I1)]
    private static extern bool dm_call_enable_mic(nint call);

    [DllImport(Dll, CallingConvention = CallingConvention.Cdecl)]
    private static extern void dm_call_set_muted(nint call, [MarshalAs(UnmanagedType.I1)] bool muted);

    [DllImport(Dll, CallingConvention = CallingConvention.Cdecl)]
    private static extern void dm_call_set_deafened(nint call, [MarshalAs(UnmanagedType.I1)] bool deafened);

    [DllImport(Dll, CallingConvention = CallingConvention.Cdecl)]
    private static extern void dm_call_stop(nint call);
}
