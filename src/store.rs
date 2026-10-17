use crate::library::{self, Item, Library};
use gpui::{App, AppContext as _, Global};
use std::path::{Path, PathBuf};

pub struct Store {
    pub lib: Library,
    pub items: Vec<Item>,
    pub loaded: bool,
}

impl Global for Store {}

pub fn init(cx: &mut App) {
    let lib = Library::new(Library::default_root());
    cx.set_global(Store { lib: lib.clone(), items: Vec::new(), loaded: false });
    let scan = cx.background_spawn(async move { lib.scan() });
    cx.spawn(async move |cx| {
        let items = scan.await;
        let _ = cx.update(|cx| {
            let store = cx.global_mut::<Store>();
            for item in items {
                if !store.items.iter().any(|i| i.dir == item.dir) {
                    store.items.push(item);
                }
            }
            store.items.sort_by(|a, b| b.meta.created.cmp(&a.meta.created));
            store.loaded = true;
        });
    })
    .detach();
}

pub fn library(cx: &App) -> Library {
    cx.global::<Store>().lib.clone()
}

pub fn get(dir: &Path, cx: &App) -> Option<Item> {
    cx.global::<Store>().items.iter().find(|i| i.dir == dir).cloned()
}

pub fn upsert(item: Item, cx: &mut App) {
    let store = cx.global_mut::<Store>();
    match store.items.iter_mut().find(|i| i.dir == item.dir) {
        Some(slot) => *slot = item,
        None => {
            let at = store.items.iter().position(|i| i.meta.created <= item.meta.created).unwrap_or(store.items.len());
            store.items.insert(at, item);
        }
    }
}

pub fn rename(dir: &Path, title: &str, cx: &mut App) -> anyhow::Result<()> {
    let item = get(dir, cx).ok_or_else(|| anyhow::anyhow!("that capture is gone"))?;
    let renamed = library::rename(&item, title)?;
    upsert(renamed, cx);
    Ok(())
}

pub fn recycle(dirs: Vec<PathBuf>, cx: &mut App, done: impl FnOnce(anyhow::Result<()>, &mut App) + 'static) {
    let to_trash = dirs.clone();
    let task = cx.background_spawn(async move { library::recycle(&to_trash) });
    cx.spawn(async move |cx| {
        let result = task.await;
        let _ = cx.update(|cx| {
            let store = cx.global_mut::<Store>();
            store.items.retain(|i| !(dirs.contains(&i.dir) && !i.dir.exists()));
            done(result, cx);
        });
    })
    .detach();
}
