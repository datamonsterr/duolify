#[tauri::command]
fn app_name() -> &'static str {
    "Duolify"
}

#[cfg_attr(mobile, tauri::mobile_entry_point)]
pub fn run() {
    tauri::Builder::default()
        .invoke_handler(tauri::generate_handler![app_name])
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
