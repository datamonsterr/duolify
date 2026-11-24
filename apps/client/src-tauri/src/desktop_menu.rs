use crate::menu_contract::{self, MenuItemSpec};
use tauri::{
    menu::{Menu, MenuItem, PredefinedMenuItem, Submenu},
    App, Emitter, Manager, Runtime,
};

fn submenu<R: Runtime>(
    app: &App<R>,
    items: &[MenuItemSpec],
    group: &str,
    title: &str,
) -> tauri::Result<Submenu<R>> {
    let submenu = Submenu::new(app, title, true)?;
    for spec in items.iter().filter(|spec| spec.group == group) {
        let item = MenuItem::with_id(
            app,
            spec.id.as_str(),
            spec.label.as_str(),
            true,
            spec.accelerator.as_deref(),
        )?;
        submenu.append(&item)?;
    }
    Ok(submenu)
}

pub(crate) fn install<R: Runtime>(app: &App<R>) -> tauri::Result<()> {
    let items = menu_contract::items();
    let application = submenu(app, &items, "app", "Duolify")?;
    application.append(&PredefinedMenuItem::separator(app)?)?;
    application.append(&PredefinedMenuItem::quit(app, None)?)?;
    let edit = Submenu::with_items(
        app,
        "Edit",
        true,
        &[
            &PredefinedMenuItem::undo(app, None)?,
            &PredefinedMenuItem::redo(app, None)?,
            &PredefinedMenuItem::separator(app)?,
            &PredefinedMenuItem::cut(app, None)?,
            &PredefinedMenuItem::copy(app, None)?,
            &PredefinedMenuItem::paste(app, None)?,
            &PredefinedMenuItem::select_all(app, None)?,
        ],
    )?;
    let navigate = submenu(app, &items, "navigate", "Navigate")?;
    let view = submenu(app, &items, "view", "View")?;
    let help = submenu(app, &items, "help", "Help")?;
    app.set_menu(Menu::with_items(
        app,
        &[&application, &edit, &navigate, &view, &help],
    )?)?;

    app.on_menu_event(move |app, event| {
        let id = event.id().as_ref();
        if !items.iter().any(|spec| spec.id == id) {
            return;
        }
        let Some(window) = app.get_webview_window("main") else {
            return;
        };
        if id == "toggle_full_screen" {
            if let Ok(fullscreen) = window.is_fullscreen() {
                let _ = window.set_fullscreen(!fullscreen);
            }
        } else {
            let _ = window.emit("duolify-hotkey", id);
        }
    });
    Ok(())
}
