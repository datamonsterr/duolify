#[tauri::command]
fn app_name() -> &'static str {
    "Duolify"
}

// Fixed service/account names keep the IPC bridge scoped to this app's auth pair.
#[tauri::command]
fn read_credentials() -> Result<Option<String>, String> {
    #[cfg(desktop)]
    {
        let entry = keyring::Entry::new("app.duolify.client", "auth-session")
            .map_err(|_| "Secure credential store is unavailable".to_string())?;
        match entry.get_password() {
            Ok(value) => Ok(Some(value)),
            Err(keyring::Error::NoEntry) => Ok(None),
            Err(_) => Err("Secure credential store is locked or unavailable".to_string()),
        }
    }
    #[cfg(mobile)]
    Err("Secure sign-in storage is not available on this mobile build".to_string())
}

#[tauri::command]
fn write_credentials(value: Option<String>) -> Result<(), String> {
    #[cfg(desktop)]
    {
        let entry = keyring::Entry::new("app.duolify.client", "auth-session")
            .map_err(|_| "Secure credential store is unavailable".to_string())?;
        let result = match value {
            Some(value) => entry.set_password(&value),
            None => entry.delete_credential(),
        };
        match result {
            Ok(()) | Err(keyring::Error::NoEntry) => Ok(()),
            Err(_) => Err("Could not update the secure credential store".to_string()),
        }
    }
    #[cfg(mobile)]
    {
        let _ = value;
        Err("Secure sign-in storage is not available on this mobile build".to_string())
    }
}

#[cfg_attr(mobile, tauri::mobile_entry_point)]
pub fn run() {
    tauri::Builder::default()
        .invoke_handler(tauri::generate_handler![
            app_name,
            read_credentials,
            write_credentials
        ])
        .run(tauri::generate_context!())
        .expect("error while running Duolify");
}

#[cfg(test)]
mod tests {
    #[test]
    fn exposes_app_name() {
        assert_eq!(super::app_name(), "Duolify");
    }
}
