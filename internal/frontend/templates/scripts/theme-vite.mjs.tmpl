import { syncTheme, manifestPath } from "./theme.mjs";

export function plutoTheme() {
  return {
    name: "pluto-theme",
    buildStart() {
      syncTheme();
    },
    configureServer(server) {
      server.watcher.add(manifestPath);
      const changed = (name) => {
        if (name !== manifestPath) return;
        try {
          syncTheme();
          server.ws.send({ type: "full-reload" });
        } catch (error) {
          server.config.logger.error(error.message);
          server.ws.send({
            type: "error",
            err: { message: error.message, stack: "", plugin: "pluto-theme" },
          });
        }
      };
      server.watcher.on("change", changed);
      server.httpServer?.once("close", () =>
        server.watcher.off("change", changed),
      );
    },
  };
}
