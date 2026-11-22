import { useTranslation } from "react-i18next";
import { type SupportedLanguage, supportedLanguages } from "../../i18n";

export function LanguageSwitcher() {
  const { t, i18n } = useTranslation();
  const current = (i18n.resolvedLanguage ?? "en") as SupportedLanguage;
  return (
    <label className="flex items-center gap-2 text-sm">
      <span>{t("language.label")}</span>
      <select
        className="rounded border border-slate-400 bg-transparent px-2 py-1"
        value={current}
        onChange={(e) => void i18n.changeLanguage(e.target.value)}
      >
        {supportedLanguages.map((l) => (
          <option key={l} value={l}>
            {t(`language.${l}`)}
          </option>
        ))}
      </select>
    </label>
  );
}
