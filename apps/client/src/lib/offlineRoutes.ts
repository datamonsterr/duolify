/**
 * Asks the service worker to cache the route chunks an offline lesson needs (player, lesson test,
 * course list). Called when a lesson is opened or a course is downloaded, so an opened or
 * downloaded lesson still plays after the network is gone even though those chunks are not part
 * of the install-time shell. Best effort: without a service worker (dev, tests, Tauri) it is a no-op.
 */
export async function warmOfflineRoutes(
  container:
    Pick<ServiceWorkerContainer, "ready"> | undefined = typeof navigator !==
  "undefined"
    ? navigator.serviceWorker
    : undefined,
): Promise<boolean> {
  if (!container) return false;
  try {
    const registration = await container.ready;
    const worker = registration.active;
    if (!worker) return false;
    return await new Promise<boolean>((resolve) => {
      const channel = new MessageChannel();
      const timer = setTimeout(() => resolve(false), 15_000);
      channel.port1.onmessage = (event) => {
        clearTimeout(timer);
        resolve(event.data === "warmed");
      };
      worker.postMessage({ type: "warm-lesson-routes" }, [channel.port2]);
    });
  } catch {
    return false;
  }
}
