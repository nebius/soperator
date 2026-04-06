local cjson = require("cjson.safe")

local function normalize_header_value(value)
    if type(value) == "table" then
        return value[1]
    end

    return value
end

local function read_request_body()
    ngx.req.read_body()

    local body = ngx.req.get_body_data()
    if body then
        return body
    end

    local body_file = ngx.req.get_body_file()
    if not body_file then
        return nil
    end

    local handle, err = io.open(body_file, "rb")
    if not handle then
        ngx.log(ngx.ERR, "failed to read request body from ", body_file, ": ", err)
        return nil
    end

    local file_body = handle:read("*a")
    handle:close()
    return file_body
end

local cgroup_parent = normalize_header_value(ngx.req.get_headers()["Cgroup-Parent"])

if ngx.req.get_method() ~= "POST" then
    return
end

if not ngx.re.find(ngx.var.uri or "", [[^/(?:v\d+(?:\.\d+)?/)?containers/create$]], "jo") then
    return
end

if not cgroup_parent or cgroup_parent == "" then
    return
end

local body = read_request_body()
if not body or body == "" then
    body = "{}"
end

local payload = cjson.decode(body)
if type(payload) ~= "table" then
    return
end

if payload.HostConfig == nil then
    payload.HostConfig = { CgroupParent = cgroup_parent }
elseif type(payload.HostConfig) == "table" then
    local current = payload.HostConfig.CgroupParent
    if current == nil or current == "" then
        payload.HostConfig.CgroupParent = cgroup_parent
    else
        return
    end
else
    return
end

local updated_body = cjson.encode(payload)
if not updated_body then
    return
end

ngx.req.set_body_data(updated_body)
