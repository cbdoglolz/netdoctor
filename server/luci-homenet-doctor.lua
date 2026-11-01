module("luci.controller.homenet_doctor", package.seeall)

function index()
    local page = entry({"admin", "services", "homenet_doctor"}, call("open_doctor"), _("HomeNet Doctor 网络诊断"), 21)
    page.dependent = false
end

function open_doctor()
    local http = require "luci.http"
    local dispatcher = require "luci.dispatcher"
    local session = dispatcher.context.authsession
    local user = dispatcher.context.authuser
    if user ~= "root" or not session or not session:match("^[a-fA-F0-9]+$") or #session ~= 32 then
        http.status(403, "Forbidden")
        http.prepare_content("text/html; charset=utf-8")
        http.write("<!doctype html><meta name='viewport' content='width=device-width,initial-scale=1'><p>请先使用 root 管理员账号登录 iStoreOS。</p>")
        return
    end
    local host = http.getenv("HTTP_HOST") or ""
    host = host:match("^([%w%.%-]+):?%d*$")
    if not host then http.status(400, "Bad Request"); return end
    -- Cookies are scoped by host/path rather than port. Transfer the existing
    -- validated LuCI session without putting it in a URL, HTML or browser storage.
    http.header("Set-Cookie", "homenet_session=" .. session .. "; Path=/; HttpOnly; SameSite=Strict; Max-Age=1800")
    http.header("Cache-Control", "no-store")
    http.redirect("http://" .. host .. ":8088/")
end
