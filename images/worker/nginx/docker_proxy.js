function normalizeHeaderValue(value) {
    if (Array.isArray(value)) {
        return value[0];
    }

    return value;
}

function copyHeaders(r, reply) {
    for (const header in reply.headersOut) {
        if (header.toLowerCase() === 'transfer-encoding') {
            continue;
        }

        r.headersOut[header] = reply.headersOut[header];
    }
}

function buildRequestBody(body, cgroupParent) {
    const payload = body ? JSON.parse(body) : {};
    if (payload === null || typeof payload !== 'object' || Array.isArray(payload)) {
        return null;
    }

    if (payload.HostConfig === undefined || payload.HostConfig === null) {
        payload.HostConfig = { CgroupParent: cgroupParent };
        return JSON.stringify(payload);
    }

    if (typeof payload.HostConfig !== 'object' || Array.isArray(payload.HostConfig)) {
        return null;
    }

    if (payload.HostConfig.CgroupParent !== undefined && payload.HostConfig.CgroupParent !== null && payload.HostConfig.CgroupParent !== '') {
        return body;
    }

    payload.HostConfig.CgroupParent = cgroupParent;
    return JSON.stringify(payload);
}

function handleCreate(r) {
    const cgroupParent = normalizeHeaderValue(r.headersIn['Cgroup-Parent']);
    if (!cgroupParent) {
        r.internalRedirect('/_docker_upstream');
        return;
    }

    let updatedBody;
    try {
        updatedBody = buildRequestBody(r.requestText, cgroupParent);
    } catch (e) {
        r.internalRedirect('/_docker_upstream');
        return;
    }

    if (updatedBody === null) {
        r.internalRedirect('/_docker_upstream');
        return;
    }

    r.variables.docker_request_uri = r.variables.request_uri;
    r.subrequest('/_docker_upstream', { method: 'POST', body: updatedBody }, function(reply) {
        copyHeaders(r, reply);
        r.return(reply.status, reply.responseText);
    });
}

export default { handleCreate };
