// Package i18n holds the user-visible strings in Brazilian Portuguese (the
// default) and English. To add a language, add a map to `catalog` and teach
// Normalize about it; the tests make sure no key is missing.
package i18n

import (
	"sort"
	"strconv"
	"strings"
)

// Supported language codes.
const (
	PT = "pt-BR"
	EN = "en"
)

// Languages lists the supported codes.
var Languages = []string{PT, EN}

// Normalize maps any language tag to a supported code (PT by default).
func Normalize(tag string) string {
	tag = strings.ToLower(strings.TrimSpace(tag))
	if tag == "en" || strings.HasPrefix(tag, "en-") || strings.HasPrefix(tag, "en_") {
		return EN
	}
	return PT
}

// Supported reports whether tag is exactly one of the supported codes.
func Supported(tag string) bool { return tag == PT || tag == EN }

// Match picks the best supported language from an Accept-Language header,
// falling back to def.
func Match(header, def string) string {
	type cand struct {
		lang string
		q    float64
	}
	var cs []cand
	for _, part := range strings.Split(header, ",") {
		fields := strings.Split(strings.TrimSpace(part), ";")
		tag := strings.ToLower(strings.TrimSpace(fields[0]))
		if tag == "" || tag == "*" {
			continue
		}
		q := 1.0
		for _, f := range fields[1:] {
			f = strings.TrimSpace(f)
			if strings.HasPrefix(f, "q=") {
				if v, err := strconv.ParseFloat(f[2:], 64); err == nil {
					q = v
				}
			}
		}
		switch {
		case tag == "en" || strings.HasPrefix(tag, "en-"):
			cs = append(cs, cand{EN, q})
		case tag == "pt" || strings.HasPrefix(tag, "pt-"):
			cs = append(cs, cand{PT, q})
		}
	}
	if len(cs) == 0 {
		return Normalize(def)
	}
	sort.SliceStable(cs, func(i, j int) bool { return cs[i].q > cs[j].q })
	return cs[0].lang
}

// T returns the string for key in lang, falling back to English, then the key.
func T(lang, key string) string {
	if m, ok := catalog[Normalize(lang)]; ok {
		if v, ok := m[key]; ok {
			return v
		}
	}
	if v, ok := catalog[EN][key]; ok {
		return v
	}
	return key
}

// Has reports whether key exists.
func Has(key string) bool { _, ok := catalog[EN][key]; return ok }

// Keys returns all keys of a language (used by tests).
func Keys(lang string) []string {
	var ks []string
	for k := range catalog[lang] {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}

var catalog = map[string]map[string]string{
	PT: {
		"nav.services": "Serviços",
		"nav.groups":   "Grupos",
		"nav.users":    "Usuários",
		"nav.devices":  "Meus dispositivos",
		"nav.logout":   "Sair",

		"login.title": "Entrar",
		"login.intro": "Informe seu e-mail cadastrado. Enviaremos um código de verificação.",
		"login.email": "E-mail",
		"login.send":  "Enviar código",

		"verify.title":  "Digite o código",
		"verify.intro":  "Se o e-mail estiver cadastrado, enviamos um código de 6 dígitos para",
		"verify.code":   "Código de verificação",
		"verify.submit": "Entrar",
		"verify.back":   "Usar outro e-mail",

		"dash.search":      "Buscar serviços…  (tecle / para focar)",
		"dash.internal":    "Interno",
		"dash.external":    "Externo",
		"dash.noresults":   "Nenhum serviço encontrado.",
		"dash.empty.title": "Nenhum serviço ainda",
		"dash.empty.text":  "Quando um administrador adicionar serviços, eles aparecerão aqui.",
		"dash.empty.cta":   "Adicionar serviços",
		"dash.ungrouped":   "Outros",

		"devices.title":    "Meus dispositivos",
		"devices.intro":    "Navegadores e aparelhos autorizados a acessar sua conta. Revogue os que você não reconhece.",
		"devices.device":   "Dispositivo",
		"devices.created":  "Autorizado em",
		"devices.lastseen": "Último acesso",
		"devices.current":  "este aparelho",
		"devices.revoke":   "Revogar",

		"admin.services.title": "Serviços",
		"admin.services.new":   "Novo serviço",
		"admin.services.edit":  "Editar serviço",
		"admin.services.none":  "Nenhum serviço cadastrado.",
		"admin.groups.title":   "Grupos",
		"admin.groups.intro":   "Grupos organizam o painel (por máquina, por tipo…). A ordem segue a posição.",
		"admin.groups.new":     "Novo grupo",
		"admin.groups.none":    "Nenhum grupo cadastrado.",
		"admin.users.title":    "Usuários",
		"admin.users.intro":    "Só quem estiver nesta lista consegue entrar. O código chega por e-mail.",
		"admin.users.new":      "Novo usuário",

		"f.name":        "Nome",
		"f.group":       "Grupo",
		"f.nogroup":     "(sem grupo)",
		"f.internal":    "URL interna",
		"f.external":    "URL externa",
		"f.description": "Descrição",
		"f.icon":        "Ícone",
		"f.icon.help":   "Um emoji ou o endereço (http/https) de uma imagem.",
		"f.position":    "Posição",
		"f.save":        "Salvar",
		"f.cancel":      "Cancelar",
		"f.edit":        "Editar",
		"f.delete":      "Excluir",
		"f.add":         "Adicionar",
		"f.email":       "E-mail",
		"f.role":        "Perfil",
		"f.confirm":     "Tem certeza?",
		"role.user":     "Usuário",
		"role.admin":    "Administrador",

		"msg.saved":      "Salvo.",
		"msg.deleted":    "Excluído.",
		"msg.revoked":    "Dispositivo revogado.",
		"msg.user_added": "Usuário adicionado.",

		"err.email":         "Informe um e-mail válido.",
		"err.code":          "Código inválido ou expirado. Peça um novo código.",
		"err.link":          "Link inválido ou expirado.",
		"err.forbidden":     "Você não tem permissão para isso.",
		"err.csrf":          "Sessão da página expirada. Recarregue e tente de novo.",
		"err.notfound":      "Página não encontrada.",
		"err.server":        "Erro interno. Veja os logs do contêiner.",
		"err.badrequest":    "Requisição inválida.",
		"err.name":          "Informe um nome (até 80 caracteres).",
		"err.description":   "Descrição muito longa (máximo 200 caracteres).",
		"err.url":           "URL inválida. Use um endereço completo começando com http:// ou https://.",
		"err.url_required":  "Informe pelo menos uma URL (interna ou externa).",
		"err.icon":          "Ícone inválido: use um emoji curto ou uma URL http(s).",
		"err.group_missing": "O grupo escolhido não existe mais.",
		"err.user_exists":   "Já existe um usuário com esse e-mail.",
		"err.user_self":     "Você não pode excluir a si mesmo.",
		"err.last_admin":    "Não é possível excluir o último administrador.",
		"err.home":          "Voltar ao início",

		"mail.code.subject": "%s é o seu código de acesso ao HomeView",
		"mail.code.body": "Olá, %s!\n\nSeu código de acesso ao HomeView é: %s\n\n" +
			"Ele vale por %d minutos. Se você não pediu este código, ignore esta mensagem.\n",
	},
	EN: {
		"nav.services": "Services",
		"nav.groups":   "Groups",
		"nav.users":    "Users",
		"nav.devices":  "My devices",
		"nav.logout":   "Sign out",

		"login.title": "Sign in",
		"login.intro": "Enter your registered e-mail. We will send you a verification code.",
		"login.email": "E-mail",
		"login.send":  "Send code",

		"verify.title":  "Enter the code",
		"verify.intro":  "If the e-mail is registered, we sent a 6-digit code to",
		"verify.code":   "Verification code",
		"verify.submit": "Sign in",
		"verify.back":   "Use another e-mail",

		"dash.search":      "Search services…  (press / to focus)",
		"dash.internal":    "Internal",
		"dash.external":    "External",
		"dash.noresults":   "No services found.",
		"dash.empty.title": "No services yet",
		"dash.empty.text":  "Once an administrator adds services, they will show up here.",
		"dash.empty.cta":   "Add services",
		"dash.ungrouped":   "Other",

		"devices.title":    "My devices",
		"devices.intro":    "Browsers and devices allowed to access your account. Revoke any you do not recognise.",
		"devices.device":   "Device",
		"devices.created":  "Authorised on",
		"devices.lastseen": "Last seen",
		"devices.current":  "this device",
		"devices.revoke":   "Revoke",

		"admin.services.title": "Services",
		"admin.services.new":   "New service",
		"admin.services.edit":  "Edit service",
		"admin.services.none":  "No services yet.",
		"admin.groups.title":   "Groups",
		"admin.groups.intro":   "Groups organise the dashboard (by machine, by type…). Order follows position.",
		"admin.groups.new":     "New group",
		"admin.groups.none":    "No groups yet.",
		"admin.users.title":    "Users",
		"admin.users.intro":    "Only people on this list can sign in. The code arrives by e-mail.",
		"admin.users.new":      "New user",

		"f.name":        "Name",
		"f.group":       "Group",
		"f.nogroup":     "(no group)",
		"f.internal":    "Internal URL",
		"f.external":    "External URL",
		"f.description": "Description",
		"f.icon":        "Icon",
		"f.icon.help":   "An emoji or the address (http/https) of an image.",
		"f.position":    "Position",
		"f.save":        "Save",
		"f.cancel":      "Cancel",
		"f.edit":        "Edit",
		"f.delete":      "Delete",
		"f.add":         "Add",
		"f.email":       "E-mail",
		"f.role":        "Role",
		"f.confirm":     "Are you sure?",
		"role.user":     "User",
		"role.admin":    "Administrator",

		"msg.saved":      "Saved.",
		"msg.deleted":    "Deleted.",
		"msg.revoked":    "Device revoked.",
		"msg.user_added": "User added.",

		"err.email":         "Enter a valid e-mail address.",
		"err.code":          "Invalid or expired code. Request a new one.",
		"err.link":          "Invalid or expired link.",
		"err.forbidden":     "You are not allowed to do that.",
		"err.csrf":          "This page expired. Reload it and try again.",
		"err.notfound":      "Page not found.",
		"err.server":        "Internal error. Check the container logs.",
		"err.badrequest":    "Invalid request.",
		"err.name":          "Enter a name (up to 80 characters).",
		"err.description":   "Description too long (200 characters max).",
		"err.url":           "Invalid URL. Use a full address starting with http:// or https://.",
		"err.url_required":  "Provide at least one URL (internal or external).",
		"err.icon":          "Invalid icon: use a short emoji or an http(s) URL.",
		"err.group_missing": "The selected group no longer exists.",
		"err.user_exists":   "A user with that e-mail already exists.",
		"err.user_self":     "You cannot delete yourself.",
		"err.last_admin":    "The last administrator cannot be deleted.",
		"err.home":          "Back to start",

		"mail.code.subject": "%s is your HomeView sign-in code",
		"mail.code.body": "Hi %s!\n\nYour HomeView sign-in code is: %s\n\n" +
			"It is valid for %d minutes. If you did not request it, ignore this message.\n",
	},
}
