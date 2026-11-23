// Shared analysis used by the lint CLI, the audit and the integration tests.
import { join } from "node:path";
import {
  catalogueViolations,
  loadConfig,
  missingKeys,
  readCatalogue,
  scanLiterals,
  sourceFiles,
  type Catalogue,
  type Config,
  type Literal,
  type Violations,
} from "./lib.ts";

export type Analysis = {
  cfg: Config;
  en: Catalogue;
  ar: Catalogue;
  literals: Literal[];
  violations: { en: Violations; ar: Violations };
  parity: { missingAr: string[]; extraAr: string[] };
};

export function analyse(root: string): Analysis {
  const cfg = loadConfig(root);
  const en: Catalogue = new Map();
  const ar: Catalogue = new Map();
  for (const c of cfg.catalogues) {
    for (const [k, v] of readCatalogue(join(root, c.file), c.en)) en.set(k, v);
    for (const [k, v] of readCatalogue(join(root, c.file), c.ar)) ar.set(k, v);
  }
  const literals = sourceFiles(root, cfg).flatMap((f) =>
    scanLiterals(f, cfg, root),
  );
  return {
    cfg,
    en,
    ar,
    literals,
    violations: {
      en: catalogueViolations(en, "en", cfg),
      ar: catalogueViolations(ar, "ar", cfg),
    },
    parity: missingKeys(en, ar),
  };
}
