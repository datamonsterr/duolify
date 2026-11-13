#[tauri::command]
fn app_name() -> &'static str {
    "Duolify"
}

const CREDENTIAL_SERVICE: &str = "app.duolify.client";
const CREDENTIAL_ACCOUNT: &str = "auth-session";

#[cfg(desktop)]
#[tauri::command]
fn read_credentials() -> Result<Option<String>, String> {
    let entry = keyring::Entry::new(CREDENTIAL_SERVICE, CREDENTIAL_ACCOUNT)
        .map_err(|_| secure_store_error())?;
    match entry.get_password() {
        Ok(value) => Ok(Some(value)),
        Err(keyring::Error::NoEntry) => Ok(None),
        Err(_) => Err(secure_store_error()),
    }
}

#[cfg(mobile)]
#[tauri::command]
fn read_credentials(app: tauri::AppHandle) -> Result<Option<String>, String> {
    use tauri_plugin_keyring_store::KeyringExt;

    app.keyring()
        .store
        .get_password(CREDENTIAL_ACCOUNT)
        .map_err(|_| secure_store_error())
}

#[cfg(desktop)]
#[tauri::command]
fn write_credentials(value: Option<String>) -> Result<(), String> {
    let entry = keyring::Entry::new(CREDENTIAL_SERVICE, CREDENTIAL_ACCOUNT)
        .map_err(|_| secure_store_error())?;
    match value {
        Some(value) => entry.set_password(&value).map_err(|_| secure_store_error()),
        None => match entry.delete_credential() {
            Ok(()) | Err(keyring::Error::NoEntry) => Ok(()),
            Err(_) => Err(secure_store_error()),
        },
    }
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
            .map_err(|_| secure_store_error()),
        None => app
            .keyring()
            .store
            .delete(CREDENTIAL_ACCOUNT)
            .map_err(|_| secure_store_error()),
    }
}

fn secure_store_error() -> String {
    "Secure credential store is locked or unavailable".to_string()
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
    #[test]
    fn exposes_app_name() {
        assert_eq!(super::app_name(), "Duolify");
    }

    #[test]
    fn secure_store_errors_are_generic_and_fail_closed() {
        assert_eq!(
            super::secure_store_error(),
            "Secure credential store is locked or unavailable"
        );
    }
}
