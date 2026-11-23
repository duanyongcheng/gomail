import { useEffect } from "react";
import { isCommandAvailable } from "@/lib/availability";
import { useTranslation } from "react-i18next";
import { setTrayMenuLabels } from "@/lib/api";
import { useUIStore } from "@/stores/ui.store";

export function useTrayI18n() {
  const language = useUIStore((s) => s.language);
  const { t } = useTranslation();

  useEffect(() => {
    if (!isCommandAvailable("set_tray_menu_labels")) return;
    setTrayMenuLabels(
      t("tray.show", "Show Window"),
      t("tray.hide", "Hide Window"),
      t("tray.quit", "Quit GoMail"),
    ).catch((err) => console.warn("Failed to sync tray menu labels", err));
  }, [language, t]);
}
