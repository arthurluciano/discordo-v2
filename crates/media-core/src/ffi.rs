//! The C interface the Windows app calls (P/Invoke).
//!
//! A call is an opaque pointer from `dm_call_start`, valid until `dm_call_stop`. Functions may be
//! called from any thread. Events come back through the callback as a JSON string
//! (`{"type": "signal", "op": ..., "sdp": ...}`, `{"type": "connected"}`, `{"type": "closed"}`),
//! on a background thread; the string is only valid during the callback.

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
    let (Some(call), false, false) = (unsafe { call.as_ref() }, typ.is_null(), sdp.is_null()) else { return };
    let (typ, sdp) = unsafe { (CStr::from_ptr(typ), CStr::from_ptr(sdp)) };
    if let (Ok(typ), Ok(sdp)) = (typ.to_str(), sdp.to_str()) {
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
