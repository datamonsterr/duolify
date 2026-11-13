#[tauri::command]
fn app_name() -> &'static str {
    "Duolify"
}

pub const CREDENTIAL_SERVICE: &str = "app.duolify.client";
pub const CREDENTIAL_ACCOUNT: &str = "auth-session";

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum BackendSelection {
    DesktopKeyring,
    MobileKeyringStore,
    Unsupported,
}

/// Pure function resolving backend selection based on target OS and runtime flags.
pub fn resolve_backend_selection(os: &str, is_desktop: bool, is_mobile: bool) -> BackendSelection {
    if is_mobile || matches!(os, "ios" | "android") {
        BackendSelection::MobileKeyringStore
    } else if is_desktop || matches!(os, "macos" | "windows" | "linux") {
        BackendSelection::DesktopKeyring
    } else {
        BackendSelection::Unsupported
    }
}

/// Compile-time active backend selection for current compilation target.
pub const fn active_backend_selection() -> BackendSelection {
    #[cfg(mobile)]
    {
        BackendSelection::MobileKeyringStore
    }
    #[cfg(all(desktop, not(mobile)))]
    {
        BackendSelection::DesktopKeyring
    }
    #[cfg(not(any(desktop, mobile)))]
    {
        BackendSelection::Unsupported
    }
}

pub fn secure_store_error() -> String {
    "Secure credential store is locked or unavailable".to_string()
}

pub fn map_mobile_keyring_store_error<E: std::fmt::Display>(_err: E) -> String {
    secure_store_error()
}

/// Minimal trait seam for credential storage adapters.
pub trait SecureStoreAdapter: Send + Sync {
    fn get_password(&self, service: &str, account: &str) -> Result<Option<String>, String>;
    fn set_password(&self, service: &str, account: &str, password: &str) -> Result<(), String>;
    fn delete_credential(&self, service: &str, account: &str) -> Result<(), String>;
}

pub fn read_credentials_with<A: SecureStoreAdapter + ?Sized>(
    adapter: &A,
    service: &str,
    account: &str,
) -> Result<Option<String>, String> {
    adapter.get_password(service, account)
}

pub fn write_credentials_with<A: SecureStoreAdapter + ?Sized>(
    adapter: &A,
    service: &str,
    account: &str,
    value: Option<String>,
) -> Result<(), String> {
    match value {
        Some(value) => adapter.set_password(service, account, &value),
        None => adapter.delete_credential(service, account),
    }
}

#[cfg(desktop)]
pub struct DesktopKeyringAdapter;

#[cfg(desktop)]
impl SecureStoreAdapter for DesktopKeyringAdapter {
    fn get_password(&self, service: &str, account: &str) -> Result<Option<String>, String> {
        let entry = keyring::Entry::new(service, account).map_err(|_| secure_store_error())?;
        match entry.get_password() {
            Ok(value) => Ok(Some(value)),
            Err(keyring::Error::NoEntry) => Ok(None),
            Err(_) => Err(secure_store_error()),
        }
    }

    fn set_password(&self, service: &str, account: &str, password: &str) -> Result<(), String> {
        let entry = keyring::Entry::new(service, account).map_err(|_| secure_store_error())?;
        entry
            .set_password(password)
            .map_err(|_| secure_store_error())
    }

    fn delete_credential(&self, service: &str, account: &str) -> Result<(), String> {
        let entry = keyring::Entry::new(service, account).map_err(|_| secure_store_error())?;
        match entry.delete_credential() {
            Ok(()) | Err(keyring::Error::NoEntry) => Ok(()),
            Err(_) => Err(secure_store_error()),
        }
    }
}

#[cfg(not(any(target_os = "ios", target_os = "android")))]
pub struct KeyringCoreAdapter;

#[cfg(not(any(target_os = "ios", target_os = "android")))]
impl SecureStoreAdapter for KeyringCoreAdapter {
    fn get_password(&self, service: &str, account: &str) -> Result<Option<String>, String> {
        let entry = keyring_core::Entry::new(service, account).map_err(|_| secure_store_error())?;
        match entry.get_password() {
            Ok(value) => Ok(Some(value)),
            Err(keyring_core::Error::NoEntry) => Ok(None),
            Err(_) => Err(secure_store_error()),
        }
    }

    fn set_password(&self, service: &str, account: &str, password: &str) -> Result<(), String> {
        let entry = keyring_core::Entry::new(service, account).map_err(|_| secure_store_error())?;
        entry
            .set_password(password)
            .map_err(|_| secure_store_error())
    }

    fn delete_credential(&self, service: &str, account: &str) -> Result<(), String> {
        let entry = keyring_core::Entry::new(service, account).map_err(|_| secure_store_error())?;
        match entry.delete_credential() {
            Ok(()) | Err(keyring_core::Error::NoEntry) => Ok(()),
            Err(_) => Err(secure_store_error()),
        }
    }
}

pub struct UnsupportedStoreAdapter;

impl SecureStoreAdapter for UnsupportedStoreAdapter {
    fn get_password(&self, _service: &str, _account: &str) -> Result<Option<String>, String> {
        Err(secure_store_error())
    }

    fn set_password(&self, _service: &str, _account: &str, _password: &str) -> Result<(), String> {
        Err(secure_store_error())
    }

    fn delete_credential(&self, _service: &str, _account: &str) -> Result<(), String> {
        Err(secure_store_error())
    }
}

#[cfg(desktop)]
#[tauri::command]
fn read_credentials() -> Result<Option<String>, String> {
    read_credentials_with(
        &DesktopKeyringAdapter,
        CREDENTIAL_SERVICE,
        CREDENTIAL_ACCOUNT,
    )
}

#[cfg(mobile)]
#[tauri::command]
fn read_credentials(app: tauri::AppHandle) -> Result<Option<String>, String> {
    use tauri_plugin_keyring_store::KeyringExt;

    app.keyring()
        .store
        .get_password(CREDENTIAL_ACCOUNT)
        .map_err(map_mobile_keyring_store_error)
}

#[cfg(desktop)]
#[tauri::command]
fn write_credentials(value: Option<String>) -> Result<(), String> {
    write_credentials_with(
        &DesktopKeyringAdapter,
        CREDENTIAL_SERVICE,
        CREDENTIAL_ACCOUNT,
        value,
    )
}

#[cfg(mobile)]
#[tauri::command]
fn write_credentials(app: tauri::AppHandle, value: Option<String>) -> Result<(), String> {
    use tauri_plugin_keyring_store::KeyringExt;

    match value {
        Some(value) => app
            .keyring()
            .store
            .set_password(CREDENTIAL_ACCOUNT, &value)
            .map_err(map_mobile_keyring_store_error),
        None => app
            .keyring()
            .store
            .delete(CREDENTIAL_ACCOUNT)
            .map_err(map_mobile_keyring_store_error),
    }
}

#[cfg(all(not(desktop), not(mobile)))]
#[tauri::command]
fn read_credentials() -> Result<Option<String>, String> {
    read_credentials_with(
        &UnsupportedStoreAdapter,
        CREDENTIAL_SERVICE,
        CREDENTIAL_ACCOUNT,
    )
}

#[cfg(all(not(desktop), not(mobile)))]
#[tauri::command]
fn write_credentials(value: Option<String>) -> Result<(), String> {
    write_credentials_with(
        &UnsupportedStoreAdapter,
        CREDENTIAL_SERVICE,
        CREDENTIAL_ACCOUNT,
        value,
    )
}

#[cfg(target_os = "ios")]
fn mobile_secure_store_plugin<R: tauri::Runtime>() -> tauri::plugin::TauriPlugin<R> {
    use tauri_plugin_keyring_store::{Builder, WriteAccessibility};

    Builder::new()
        .service(CREDENTIAL_SERVICE)
        .ios_write_accessibility(WriteAccessibility::AfterFirstUnlockThisDeviceOnly)
        .build()
}

#[cfg(target_os = "android")]
fn mobile_secure_store_plugin<R: tauri::Runtime>() -> tauri::plugin::TauriPlugin<R> {
    tauri_plugin_keyring_store::Builder::new()
        .service(CREDENTIAL_SERVICE)
        .build()
}

#[cfg_attr(mobile, tauri::mobile_entry_point)]
pub fn run() {
    let builder = tauri::Builder::default();

    #[cfg(desktop)]
    let builder = builder.plugin(tauri_plugin_single_instance::init(|_app, _argv, _cwd| {}));

    let builder = builder
        .plugin(tauri_plugin_opener::init())
        .plugin(tauri_plugin_deep_link::init())
        .invoke_handler(tauri::generate_handler![
            app_name,
            read_credentials,
            write_credentials
        ]);

    #[cfg(mobile)]
    let builder = builder.plugin(mobile_secure_store_plugin());

    builder
        .run(tauri::generate_context!())
        .expect("error while running Duolify");
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn exposes_app_name() {
        assert_eq!(app_name(), "Duolify");
    }

    #[test]
    fn secure_store_errors_are_generic_and_fail_closed() {
        assert_eq!(
            secure_store_error(),
            "Secure credential store is locked or unavailable"
        );
    }

    #[test]
    fn backend_selection_desktop_linux_macos_windows() {
        assert_eq!(
            resolve_backend_selection("linux", true, false),
            BackendSelection::DesktopKeyring
        );
        assert_eq!(
            resolve_backend_selection("macos", true, false),
            BackendSelection::DesktopKeyring
        );
        assert_eq!(
            resolve_backend_selection("windows", true, false),
            BackendSelection::DesktopKeyring
        );
    }

    #[test]
    fn backend_selection_mobile_ios_android() {
        assert_eq!(
            resolve_backend_selection("ios", false, true),
            BackendSelection::MobileKeyringStore
        );
        assert_eq!(
            resolve_backend_selection("android", false, true),
            BackendSelection::MobileKeyringStore
        );
    }

    #[test]
    fn backend_selection_unsupported_platforms_fail_closed() {
        assert_eq!(
            resolve_backend_selection("freebsd", false, false),
            BackendSelection::Unsupported
        );
        assert_eq!(
            resolve_backend_selection("solaris", false, false),
            BackendSelection::Unsupported
        );
        assert_eq!(
            resolve_backend_selection("wasm32", false, false),
            BackendSelection::Unsupported
        );
        assert_eq!(
            resolve_backend_selection("unknown-os", false, false),
            BackendSelection::Unsupported
        );
    }

    #[test]
    fn active_backend_selection_matches_build_target() {
        #[cfg(desktop)]
        assert_eq!(active_backend_selection(), BackendSelection::DesktopKeyring);
        #[cfg(mobile)]
        assert_eq!(
            active_backend_selection(),
            BackendSelection::MobileKeyringStore
        );
        #[cfg(all(not(desktop), not(mobile)))]
        assert_eq!(active_backend_selection(), BackendSelection::Unsupported);
    }

    #[test]
    fn unsupported_platform_adapter_fails_closed() {
        let adapter = UnsupportedStoreAdapter;
        let read_result = read_credentials_with(&adapter, CREDENTIAL_SERVICE, CREDENTIAL_ACCOUNT);
        assert!(read_result.is_err());
        assert_eq!(read_result.unwrap_err(), secure_store_error());

        let write_result = write_credentials_with(
            &adapter,
            CREDENTIAL_SERVICE,
            CREDENTIAL_ACCOUNT,
            Some("plaintext-leak-attempt".to_string()),
        );
        assert!(write_result.is_err());
        assert_eq!(write_result.unwrap_err(), secure_store_error());

        let delete_result =
            write_credentials_with(&adapter, CREDENTIAL_SERVICE, CREDENTIAL_ACCOUNT, None);
        assert!(delete_result.is_err());
        assert_eq!(delete_result.unwrap_err(), secure_store_error());
    }

    #[test]
    fn fail_closed_store_never_persists_plaintext_fallback() {
        struct FailingAdapter;
        impl SecureStoreAdapter for FailingAdapter {
            fn get_password(&self, _s: &str, _a: &str) -> Result<Option<String>, String> {
                Err(secure_store_error())
            }
            fn set_password(&self, _s: &str, _a: &str, _p: &str) -> Result<(), String> {
                Err(secure_store_error())
            }
            fn delete_credential(&self, _s: &str, _a: &str) -> Result<(), String> {
                Err(secure_store_error())
            }
        }

        let adapter = FailingAdapter;
        let secret = "sensitive-jwt-token-12345";

        let write_res = write_credentials_with(
            &adapter,
            CREDENTIAL_SERVICE,
            CREDENTIAL_ACCOUNT,
            Some(secret.to_string()),
        );
        assert_eq!(write_res.unwrap_err(), secure_store_error());

        let read_res = read_credentials_with(&adapter, CREDENTIAL_SERVICE, CREDENTIAL_ACCOUNT);
        assert_eq!(read_res.unwrap_err(), secure_store_error());

        let del_res =
            write_credentials_with(&adapter, CREDENTIAL_SERVICE, CREDENTIAL_ACCOUNT, None);
        assert_eq!(del_res.unwrap_err(), secure_store_error());

        // Verify that no file in the current working directory contains the secret
        let current_dir = std::fs::read_dir(".").expect("readable cwd");
        for entry in current_dir.flatten() {
            if entry.file_type().map(|t| t.is_file()).unwrap_or(false) {
                if let Ok(content) = std::fs::read_to_string(entry.path()) {
                    assert!(
                        !content.contains(secret),
                        "Found plaintext secret persisted in cwd: {:?}",
                        entry.path()
                    );
                }
            }
        }
    }

    #[test]
    fn mobile_keyring_store_error_mapping_fails_closed() {
        let err_str = map_mobile_keyring_store_error("KeychainLocked: os_status -25308");
        assert_eq!(err_str, secure_store_error());
        assert!(!err_str.contains("KeychainLocked"));
        assert!(!err_str.contains("-25308"));

        let err_fmt = map_mobile_keyring_store_error(std::io::Error::new(
            std::io::ErrorKind::PermissionDenied,
            "keystore failure",
        ));
        assert_eq!(err_fmt, secure_store_error());
    }

    #[test]
    fn desktop_mock_keyring_round_trip_sign_in() {
        let mock_store = keyring_core::mock::Store::new().unwrap();
        keyring_core::set_default_store(mock_store);
        let adapter = KeyringCoreAdapter;

        // Starts signed out (empty)
        let initial = read_credentials_with(&adapter, CREDENTIAL_SERVICE, CREDENTIAL_ACCOUNT);
        assert_eq!(initial.unwrap(), None);

        // Sign in persists session tokens
        let session_tokens = r#"{"access_token":"acc-1","refresh_token":"ref-1","expires_in":900}"#;
        let write_res = write_credentials_with(
            &adapter,
            CREDENTIAL_SERVICE,
            CREDENTIAL_ACCOUNT,
            Some(session_tokens.to_string()),
        );
        assert!(write_res.is_ok());

        // Subsequent read returns stored session tokens
        let read_res = read_credentials_with(&adapter, CREDENTIAL_SERVICE, CREDENTIAL_ACCOUNT);
        assert_eq!(read_res.unwrap(), Some(session_tokens.to_string()));
    }

    #[test]
    fn desktop_mock_keyring_refresh_token_rotation_replaces_credentials() {
        let mock_store = keyring_core::mock::Store::new().unwrap();
        keyring_core::set_default_store(mock_store);
        let adapter = KeyringCoreAdapter;

        let tokens_v1 = r#"{"access_token":"acc-old","refresh_token":"ref-old","expires_in":900}"#;
        write_credentials_with(
            &adapter,
            CREDENTIAL_SERVICE,
            CREDENTIAL_ACCOUNT,
            Some(tokens_v1.to_string()),
        )
        .unwrap();

        let tokens_v2 =
            r#"{"access_token":"acc-rotated","refresh_token":"ref-rotated","expires_in":900}"#;
        let rotate_res = write_credentials_with(
            &adapter,
            CREDENTIAL_SERVICE,
            CREDENTIAL_ACCOUNT,
            Some(tokens_v2.to_string()),
        );
        assert!(rotate_res.is_ok());

        let read_res =
            read_credentials_with(&adapter, CREDENTIAL_SERVICE, CREDENTIAL_ACCOUNT).unwrap();
        assert_eq!(read_res, Some(tokens_v2.to_string()));
        assert_ne!(read_res, Some(tokens_v1.to_string()));
    }

    #[test]
    fn desktop_mock_keyring_sign_out_clears_credentials_idempotently() {
        let mock_store = keyring_core::mock::Store::new().unwrap();
        keyring_core::set_default_store(mock_store);
        let adapter = KeyringCoreAdapter;

        let session = r#"{"access_token":"acc-live","refresh_token":"ref-live"}"#;
        write_credentials_with(
            &adapter,
            CREDENTIAL_SERVICE,
            CREDENTIAL_ACCOUNT,
            Some(session.to_string()),
        )
        .unwrap();
        assert!(
            read_credentials_with(&adapter, CREDENTIAL_SERVICE, CREDENTIAL_ACCOUNT)
                .unwrap()
                .is_some()
        );

        // Sign out clears credentials
        let sign_out_res =
            write_credentials_with(&adapter, CREDENTIAL_SERVICE, CREDENTIAL_ACCOUNT, None);
        assert!(sign_out_res.is_ok());
        assert_eq!(
            read_credentials_with(&adapter, CREDENTIAL_SERVICE, CREDENTIAL_ACCOUNT).unwrap(),
            None
        );

        // Subsequent sign out is idempotent (NoEntry treated as success)
        let repeat_sign_out =
            write_credentials_with(&adapter, CREDENTIAL_SERVICE, CREDENTIAL_ACCOUNT, None);
        assert!(repeat_sign_out.is_ok());
        assert_eq!(
            read_credentials_with(&adapter, CREDENTIAL_SERVICE, CREDENTIAL_ACCOUNT).unwrap(),
            None
        );
    }

    #[test]
    fn desktop_mock_keyring_injected_error_fails_closed() {
        let mock_store = keyring_core::mock::Store::new().unwrap();
        keyring_core::set_default_store(mock_store);
        let adapter = KeyringCoreAdapter;

        let entry = keyring_core::Entry::new(CREDENTIAL_SERVICE, CREDENTIAL_ACCOUNT).unwrap();
        let mock: &keyring_core::mock::Cred = entry.as_any().downcast_ref().unwrap();

        // Inject error into mock credential
        mock.set_error(keyring_core::Error::PlatformFailure(Box::new(
            std::io::Error::new(
                std::io::ErrorKind::PermissionDenied,
                "OS keychain denied access",
            ),
        )));

        // Read fails closed
        let read_err = read_credentials_with(&adapter, CREDENTIAL_SERVICE, CREDENTIAL_ACCOUNT);
        assert!(read_err.is_err());
        assert_eq!(read_err.unwrap_err(), secure_store_error());

        // Set error again for write
        mock.set_error(keyring_core::Error::PlatformFailure(Box::new(
            std::io::Error::new(std::io::ErrorKind::Other, "Disk quota full"),
        )));
        let write_err = write_credentials_with(
            &adapter,
            CREDENTIAL_SERVICE,
            CREDENTIAL_ACCOUNT,
            Some("token".to_string()),
        );
        assert!(write_err.is_err());
        assert_eq!(write_err.unwrap_err(), secure_store_error());

        // Set error again for delete
        mock.set_error(keyring_core::Error::PlatformFailure(Box::new(
            std::io::Error::new(std::io::ErrorKind::Other, "Store corrupted"),
        )));
        let del_err =
            write_credentials_with(&adapter, CREDENTIAL_SERVICE, CREDENTIAL_ACCOUNT, None);
        assert!(del_err.is_err());
        assert_eq!(del_err.unwrap_err(), secure_store_error());
    }
}
