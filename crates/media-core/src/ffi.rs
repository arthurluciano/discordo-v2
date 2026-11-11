//! The C interface the Windows app calls (P/Invoke).
//!
//! A call is an opaque pointer from `dm_call_start`, valid until `dm_call_stop`. Functions may be
//! called from any thread. Events come back through the callback as a JSON string, on a background
//! thread; the string is only valid during the callback:
//!
//! - `{"type": "signal", "op", "sdp"}`: send `{"op": op, "d": {"sdp": sdp}}` over the gateway
//! - `{"type": "connected"}` / `{"type": "closed"}`
//! - `{"type": "video", "user", "track", "width", "height"}`: picture size of a watched video
//! - `{"type": "share_stopped"}`: our screen share ended on its own

use std::ffi::{CStr, CString, c_char, c_void};

use serde_json::json;

use crate::call::{Call, CallEvent};

pub type EventCallback = extern "C" fn(context: *mut c_void, json: *const c_char);

/// The app's context pointer, handed back untouched; the app promises it is usable from any thread.
struct Context(*mut c_void);
unsafe impl Send for Context {}
unsafe impl Sync for Context {}

/// Starts a call. Returns null if the audio output or the network could not be set up.
#[unsafe(no_mangle)]
pub extern "C" fn dm_call_start(callback: EventCallback, context: *mut c_void) -> *mut Call {
    let context = Context(context);
    let call = Call::start(move |event| {
        let context = &context; // capture the wrapper, not the raw pointer inside it
        let message = match event {
            CallEvent::Signal { op, sdp } => json!({ "type": "signal", "op": op, "sdp": sdp }),
            CallEvent::Connected => json!({ "type": "connected" }),
            CallEvent::Closed => json!({ "type": "closed" }),
            CallEvent::Video { user, track, width, height } => {
                json!({ "type": "video", "user": user, "track": track, "width": width, "height": height })
            }
            CallEvent::ShareStopped => json!({ "type": "share_stopped" }),
        };
        if let Ok(text) = CString::new(message.to_string()) {
            callback(context.0, text.as_ptr());
        }
    });
    match call {
        Ok(call) => Box::into_raw(Box::new(call)),
        Err(_) => std::ptr::null_mut(),
    }
}

/// Feeds a signaling message from the gateway: `typ` is "RTC_ANSWER" or "RTC_OFFER".
///
/// # Safety
/// `call` must come from `dm_call_start` and not have been stopped; the strings must be NUL-terminated.
#[unsafe(no_mangle)]
pub unsafe extern "C" fn dm_call_signal(call: *const Call, typ: *const c_char, sdp: *const c_char) {
    if let Some((call, typ, sdp)) = unsafe { call_and_strings(call, typ, sdp) } {
        call.signal(typ, sdp);
    }
}

/// Opens the microphone and starts sending it. Returns false if there is no usable microphone.
///
/// # Safety
/// `call` must come from `dm_call_start` and not have been stopped.
#[unsafe(no_mangle)]
pub unsafe extern "C" fn dm_call_enable_mic(call: *const Call) -> bool {
    let Some(call) = (unsafe { call.as_ref() }) else { return false };
    #[cfg(windows)]
    return call.enable_mic().is_ok();
    #[cfg(not(windows))]
    {
        let _ = call;
        false
    }
}

/// # Safety
/// `call` must come from `dm_call_start` and not have been stopped.
#[unsafe(no_mangle)]
pub unsafe extern "C" fn dm_call_set_muted(call: *const Call, muted: bool) {
    if let Some(call) = unsafe { call.as_ref() } {
        call.set_muted(muted);
    }
}

/// # Safety
/// `call` must come from `dm_call_start` and not have been stopped.
#[unsafe(no_mangle)]
pub unsafe extern "C" fn dm_call_set_deafened(call: *const Call, deafened: bool) {
    if let Some(call) = unsafe { call.as_ref() } {
        call.set_deafened(deafened);
    }
}

/// Starts (or restarts with new settings) sharing the primary screen, at up to `height` lines and
/// `fps` frames per second. Returns 1 when the GPU encodes, 2 when the CPU does, 0 on failure.
///
/// # Safety
/// `call` must come from `dm_call_start` and not have been stopped.
#[unsafe(no_mangle)]
pub unsafe extern "C" fn dm_call_share_screen(call: *const Call, height: u32, fps: u32) -> i32 {
    let Some(call) = (unsafe { call.as_ref() }) else { return 0 };
    #[cfg(windows)]
    return match call.share_screen(height, fps) {
        Ok(true) => 1,
        Ok(false) => 2,
        Err(_) => 0,
    };
    #[cfg(not(windows))]
    {
        let _ = (call, height, fps);
        0
    }
}

/// # Safety
/// `call` must come from `dm_call_start` and not have been stopped.
#[unsafe(no_mangle)]
pub unsafe extern "C" fn dm_call_stop_share(call: *const Call) {
    #[cfg(windows)]
    if let Some(call) = unsafe { call.as_ref() } {
        call.stop_share();
    }
    #[cfg(not(windows))]
    let _ = call;
}

/// Starts showing a video track (`track` is "screen" or "camera") of `user`. Returns an
/// `IDXGISwapChain1*` to hand to a SwapChainPanel, or null on failure. The caller owns one
/// reference and must release it.
///
/// # Safety
/// `call` must come from `dm_call_start` and not have been stopped; the strings must be NUL-terminated.
#[unsafe(no_mangle)]
pub unsafe extern "C" fn dm_call_watch(call: *const Call, user: *const c_char, track: *const c_char) -> *mut c_void {
    let Some((call, user, track)) = (unsafe { call_and_strings(call, user, track) }) else { return std::ptr::null_mut() };
    #[cfg(windows)]
    return call.watch(user, track).unwrap_or(std::ptr::null_mut());
    #[cfg(not(windows))]
    {
        let _ = (call, user, track);
        std::ptr::null_mut()
    }
}

/// # Safety
/// `call` must come from `dm_call_start` and not have been stopped; the strings must be NUL-terminated.
#[unsafe(no_mangle)]
pub unsafe extern "C" fn dm_call_unwatch(call: *const Call, user: *const c_char, track: *const c_char) {
    if let Some((call, user, track)) = unsafe { call_and_strings(call, user, track) } {
        call.unwatch(user, track);
    }
}

unsafe fn call_and_strings<'a>(call: *const Call, a: *const c_char, b: *const c_char) -> Option<(&'a Call, &'a str, &'a str)> {
    if a.is_null() || b.is_null() {
        return None;
    }
    let call = unsafe { call.as_ref() }?;
    let (a, b) = unsafe { (CStr::from_ptr(a), CStr::from_ptr(b)) };
    Some((call, a.to_str().ok()?, b.to_str().ok()?))
}

/// Ends the call and frees it. A callback already running on another thread may still finish after this returns.
///
/// # Safety
/// `call` must come from `dm_call_start`; it must not be used again.
#[unsafe(no_mangle)]
pub unsafe extern "C" fn dm_call_stop(call: *mut Call) {
    if !call.is_null() {
        drop(unsafe { Box::from_raw(call) });
    }
}
