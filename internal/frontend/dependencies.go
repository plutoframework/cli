package frontend

import "github.com/plutoframework/cli/internal/project"

const NodeVersion = "24.22.1"

func dependencies(m project.Manifest) map[string]map[string]string {
	runtime := map[string]string{
		"react":                  "19.3.1",
		"react-dom":              "19.3.1",
		"@tanstack/react-query":  "5.105.0",
		"@tanstack/react-router": "1.170.46",
		"openapi-fetch":          "0.17.0",
	}
	dev := map[string]string{
		"vite":                        "8.3.5",
		"@vitejs/plugin-react":        "6.1.2",
		"typescript":                  "5.9.3",
		"@types/react":                "19.3.0",
		"@types/react-dom":            "19.3.0",
		"@types/node":                 "24.19.2",
		"eslint":                      "10.13.0",
		"@eslint/js":                  "10.0.1",
		"typescript-eslint":           "8.72.1",
		"eslint-plugin-react-hooks":   "7.1.1",
		"prettier":                    "3.9.10",
		"vitest":                      "5.0.5",
		"jsdom":                       "30.1.3",
		"@testing-library/react":      "16.3.3",
		"@testing-library/jest-dom":   "7.0.1",
		"@testing-library/user-event": "14.6.8",
		"openapi-typescript":          "7.13.0",
	}

	f := m.Frontend
	if f.UI != "native" {
		runtime["tailwindcss"] = "4.3.3"
		dev["@tailwindcss/vite"] = "4.3.3"
	}

	if f.UI == "heroui-v3" {
		runtime["@heroui/react"] = "3.2.6"
		runtime["@heroui/styles"] = "3.2.6"
	}

	if f.UI == "shadcn" {
		runtime["clsx"] = "2.1.1"
		runtime["tailwind-merge"] = "3.7.0"

		for _, name := range requiredPrimitives(m) {
			switch name {
			case "button":
				runtime["class-variance-authority"] = "0.7.1"
				runtime["radix-ui"] = "1.8.1"
			case "label":
				runtime["radix-ui"] = "1.8.1"
			case "dialog":
				runtime["radix-ui"] = "1.8.1"
				runtime["lucide-react"] = "1.62.0"
			}
		}
	}

	return map[string]map[string]string{
		"dependencies":    runtime,
		"devDependencies": dev,
	}
}

func packageJSON(m project.Manifest) ([]byte, error) {
	d := dependencies(m)

	return jsonBytes(map[string]any{
		"name":    m.Project.Name + "-web",
		"private": true,
		"version": "0.1.0",
		"type":    "module",
		"engines": map[string]string{"node": ">=24.15 <25"},
		"scripts": map[string]string{
			"dev":          "node scripts/certificate.mjs && vite",
			"build":        "tsc --noEmit && vite build",
			"preview":      "node scripts/certificate.mjs && vite preview",
			"api:generate": "node scripts/generate-api.mjs",
			"api:check":    "node scripts/generate-api.mjs --check",
			"fmt":          "prettier --write .",
			"lint":         "eslint . && prettier --check . && node scripts/architecture.mjs && node scripts/check.mjs",
			"test":         "vitest run --passWithNoTests && node --test scripts/architecture.test.mjs",
			"check":        "npm run api:check && npm run lint && tsc --noEmit && npm test && npm run build",
		},
		"dependencies":    d["dependencies"],
		"devDependencies": d["devDependencies"],
	})
}
