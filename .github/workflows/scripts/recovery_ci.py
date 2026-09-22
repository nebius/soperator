#!/usr/bin/env python3
"""Prepared recovery payload. Run only in the explicitly approved temporary workflow."""
import base64
import hashlib
import json
import os
import subprocess
import sys
import urllib.error
import urllib.parse
import urllib.request

CONFIG = json.loads(r'''{"actions":[{"ref":"ghcr.io/nebius/soperator/controller_slurmctld:4.1.8-slurm25.11.3","expected_current_digest":"sha256:d45cc88e4bdefa1dc9d4d095cb68a571b5af80976c37d1fdeedfe8d58efb9b87","restore_digest":"sha256:6a188de71bbca6da49f1a5cdf3c7f9865716b1622f46b91f856339c3d03af881","content_type":"application/vnd.oci.image.index.v1+json"},{"ref":"ghcr.io/nebius/soperator/controller_slurmdbd:4.1.8-slurm25.11.3","expected_current_digest":"sha256:8a9ab08c6f1e22d3c31244ae999ac87771f589b8a8b00258c5faf99a4d6ef9ac","restore_digest":"sha256:788eec034bb01058d4a818d7adb6cf5548fcd72c67b6581dbb705b8077439a79","content_type":"application/vnd.oci.image.index.v1+json"},{"ref":"ghcr.io/nebius/soperator/k8s_check_job:4.1.8-slurm25.11.3","expected_current_digest":"sha256:e23cc9daf356ddd9f9ddca62aab4dda04f0b4e6c26938976480f72c1c87353e6","restore_digest":"sha256:e96245a59686a35a541d77cc31cf624b7bcac7edf555a0ef4b6faa4baec20903","content_type":"application/vnd.oci.image.index.v1+json"},{"ref":"ghcr.io/nebius/soperator/login_sshd:4.1.8-slurm25.11.3","expected_current_digest":"sha256:6617a8e9542d915d49180591a9d246c0c7edb408b51a6f490f7793f28b60b604","restore_digest":"sha256:a867dd96a5d07a6445607f04481b24162b4c04bf04d6bd645a7b4a3b55b13f20","content_type":"application/vnd.oci.image.index.v1+json"},{"ref":"ghcr.io/nebius/soperator/munge:4.1.8-slurm25.11.3","expected_current_digest":"sha256:c461979809f1f97b256755e780c01b06c284f31c610c71ccf2ef8952f681e3ef","restore_digest":"sha256:481cbb7f425617a96dc74e7587daf699074de3231731c20698ee5f28f115aade","content_type":"application/vnd.oci.image.index.v1+json"},{"ref":"ghcr.io/nebius/soperator/populate_jail:4.1.8-slurm25.11.3-cuda12.9.0","expected_current_digest":"sha256:b5d653deb3ec23c7f16c96af26c5bf4490a1c9e4961bf78e620a24d587835344","restore_digest":"sha256:8b172075dad3a81e078547f861810cff38cd5313804892a74e243117bf331a9a","content_type":"application/vnd.oci.image.index.v1+json"},{"ref":"ghcr.io/nebius/soperator/populate_jail:4.1.8-slurm25.11.3-cuda13.0.2","expected_current_digest":"sha256:02ab1fe7791c6318de7e75aa5d595731f6e698cbde7de5d7e4fe601f65875a62","restore_digest":"sha256:ae0a56d62ee36823f23c956f41d1c3443f00eac7b970e9735a5b3880b53a7904","content_type":"application/vnd.oci.image.index.v1+json"},{"ref":"ghcr.io/nebius/soperator/rebooter:4.1.8","expected_current_digest":"sha256:809027265ea8f6c59c1e3b8b2cef9f28704d0a9c2e523787485cc364d6b2bf98","restore_digest":"sha256:2819eb257c1b1c49ecc727eb33c7c55a20f270f9e6210a7dcd9b30f5de3569c3","content_type":"application/vnd.oci.image.index.v1+json"},{"ref":"ghcr.io/nebius/soperator/sansible:4.1.8-slurm25.11.3","expected_current_digest":"sha256:aab8fa33ad49653ca4eb935232e32b69ea781bc52e7874dde1a803e56bf7f923","restore_digest":"sha256:33139873ba021065bbd3d0adad204ce6a84acb807a51ea6734103aa52ca6bef9","content_type":"application/vnd.oci.image.index.v1+json"},{"ref":"ghcr.io/nebius/soperator/sconfigcontroller:4.1.8","expected_current_digest":"sha256:36ba2f2f9166eda879e76b36b4cd1b688e7729f7872e90250f3ab9320fcafc83","restore_digest":"sha256:ec023973a832000c08a2f3e482b3c36fdc66d22677890efe83566ee19639d053","content_type":"application/vnd.oci.image.index.v1+json"},{"ref":"ghcr.io/nebius/soperator/slurm-operator:4.1.8","expected_current_digest":"sha256:e23408ad7d8a774812ca5291faedc195ff35b57a0160856dcb65e7ac6092dd37","restore_digest":"sha256:08305ab88a3f72aa0f9ae68ba42776c53985137f4156dfd957aedad15f6856b5","content_type":"application/vnd.oci.image.index.v1+json"},{"ref":"ghcr.io/nebius/soperator/slurm_check_job:4.1.8-slurm25.11.3","expected_current_digest":"sha256:800aecd390c3befe3258c38a6db3780675f99933fede06d7777bd9e993805230","restore_digest":"sha256:671f675bf0e998da4a6c53e811b01ce474470cd1fcf8d69949de601bdca554c7","content_type":"application/vnd.oci.image.index.v1+json"},{"ref":"ghcr.io/nebius/soperator/slurmrestd:4.1.8-slurm25.11.3","expected_current_digest":"sha256:75c547f95b62e5d6f379fdbc4024fc85bca733313f8ecf886cf89abc94ffe7d5","restore_digest":"sha256:f498b8e3fc5ee19c30cdaf73651a72195a1c1789430e696799821eb016224165","content_type":"application/vnd.oci.image.index.v1+json"},{"ref":"ghcr.io/nebius/soperator/soperator-exporter:4.1.8-slurm25.11.3","expected_current_digest":"sha256:0216e0c72097e355b35a0c12fd6198f0b6b4e7283802545802b49e2025734c6c","restore_digest":"sha256:fa2a7087f69667c039351780d457ba924cfc6645c14cf0b93607db40161951f0","content_type":"application/vnd.oci.image.index.v1+json"},{"ref":"ghcr.io/nebius/soperator/soperatorchecks:4.1.8","expected_current_digest":"sha256:fdacc17fdc4e889949a951706f839873289117616e2094a1edd4a37d87e6e9c8","restore_digest":"sha256:c9bc14b670de8522d3fdc26c4edfbdda420d20d53b78d9d3d0f73e77197f4c3b","content_type":"application/vnd.oci.image.index.v1+json"},{"ref":"ghcr.io/nebius/soperator/worker_slurmd:4.1.8-slurm25.11.3","expected_current_digest":"sha256:1265f6be76a17c6e7f41711610857b89ff93e6a0b2345377b88724ac38bf1b92","restore_digest":"sha256:1ca64768fdf356bb402efe337ce1ae67d61e2820c0ca9dcf1fd19393da83d0b6","content_type":"application/vnd.oci.image.index.v1+json"}],"protected":[{"ref":"ghcr.io/nebius/soperator/controller_slurmctld:4.1.9-slurm25.11.3","status":200,"digest":"sha256:ed676150ea9ce25443b140b663a7e50ee273d483584f5206bce72a32d2dc6741"},{"ref":"ghcr.io/nebius/soperator/controller_slurmdbd:4.1.9-slurm25.11.3","status":200,"digest":"sha256:e56d87237ad9dbeca5b46733c65343d715581647cb3b9510f4eca7beac8ac8e7"},{"ref":"ghcr.io/nebius/soperator/k8s_check_job:4.1.9-slurm25.11.3","status":200,"digest":"sha256:276171bbf4b1b5a367715d0e159e5a5d423831d6bd278d98e2dfef96c42a4e8c"},{"ref":"ghcr.io/nebius/soperator/login_sshd:4.1.9-slurm25.11.3","status":200,"digest":"sha256:b97335bcba3f2335100b81a50e44de486418a101aac732130c018f8211e6e3c4"},{"ref":"ghcr.io/nebius/soperator/munge:4.1.9-slurm25.11.3","status":200,"digest":"sha256:c6d9962f05b08483a7cbc84eda27ddcd5c84b1656c9d5ffb24ddf1e6562465ef"},{"ref":"ghcr.io/nebius/soperator/nfs-server:1.2.0","status":200,"digest":"sha256:a8c1377dd1f12e4893b7b3aba33e5490eb18743722c60d0c8bce921312c1e7fc"},{"ref":"ghcr.io/nebius/soperator/populate_jail:4.1.9-slurm25.11.3-cuda12.9.0","status":200,"digest":"sha256:00b4a938787c5ffcd634913b90f9805bcf76693c186149a30334572ef4764e5f"},{"ref":"ghcr.io/nebius/soperator/populate_jail:4.1.9-slurm25.11.3-cuda13.0.2","status":200,"digest":"sha256:e82baa19ca1cc02fd49ff4fba30f4cb6158409424b5beeb08104416cb3afa48b"},{"ref":"ghcr.io/nebius/soperator/rebooter:4.1.9","status":200,"digest":"sha256:07f7b256fca37e8dcae4fa816d3675fe69b03fba3d2b142f0314bcc1966595b0"},{"ref":"ghcr.io/nebius/soperator/sansible:4.1.9-slurm25.11.3","status":200,"digest":"sha256:877b567668e6e83eef75e7f5be140fd36a091faab71512eab114db6a61fba524"},{"ref":"ghcr.io/nebius/soperator/sconfigcontroller:4.1.9","status":200,"digest":"sha256:372a0f5678dcd1f71ad2c2d28a56bee830964fd510b61287c52110c35fb09aa6"},{"ref":"ghcr.io/nebius/soperator/slurm-operator:4.1.9","status":200,"digest":"sha256:4e3956c4d1d57b203d267aa3af2f0626415b15bf6bb1914b7d4ff193f7fe9757"},{"ref":"ghcr.io/nebius/soperator/slurm_check_job:4.1.9-slurm25.11.3","status":200,"digest":"sha256:86cf6bef9c8b1351479d551f61afe942c0ed3c8e2cb806154264f32ea47eb977"},{"ref":"ghcr.io/nebius/soperator/slurmrestd:4.1.9-slurm25.11.3","status":200,"digest":"sha256:63c955ae650cb5e83c71cec6b771b992045d2ad6e096122b07d327e648cdb3a4"},{"ref":"ghcr.io/nebius/soperator/soperator-exporter:4.1.9-slurm25.11.3","status":200,"digest":"sha256:f7b10abafab2d48427d65e01ae03828bb28f3093712635017c73c6b39d243f8b"},{"ref":"ghcr.io/nebius/soperator/soperatorchecks:4.1.9","status":200,"digest":"sha256:5140e79304b416ae4cd6ca6398b50acd7901a65b27251879ddd2e36e5c114389"},{"ref":"ghcr.io/nebius/soperator/worker_slurmd:4.1.9-slurm25.11.3","status":200,"digest":"sha256:3ad794c9321721094653081885c6e0750e6e8b051abbc51a5616f7217b728ebd"}],"git_tag":{"name":"4.1.8","expected_current_sha":"50923ae721075e041e3ae5ccf4782cc34e52c4ca","restore_sha":"5860baa4c6aed178e057f1d6b982b15bb31eb0a1","method":"Temporary recovery workflow GITHUB_TOKEN git push with explicit force-with-lease; do not push this tag with a user token"},"protected_git_refs":{"refs/tags/4.1.9":"98e27e4c6d11c2c5155662d478ce4e5e379f9fac","refs/heads/soperator-release-4.1":"b2568cb80ca5241c1ce22dfe6f3f6a27467c46e0"},"release_patch":{"body":"Changes made since version `4.1.7` prior to version `4.1.8`:\n\n## \ud83d\udc1b Fixes\n\n- SCHED-1520, SCHED-1616: only fail acceptance for ActiveCheck errors\n   - PR: #2932\n- SCHED-2007: select a valid E2E build\n   - PR: #2945\n- SCHED-2442: Fix local NVMe disks in 4.1\n   - PR: #2942\n\n\n\nContributors:\n@theyoprst, @github-actions[bot], @ChessProfessor, @ali-sattari\n\n| \ud83d\udcc1 **Categorized PRs** | \ud83d\udcc2 **Uncategorized PRs** | \ud83d\udce5 **Commits** | \u2795 **Lines added** | \u2796 **Lines deleted** |\n| :---: | :---: | :---: | :---: | :---: |\n| 210 | 0 | 20 | 1592 | 338 |\n","make_latest":"false"},"release418_id":385365642,"release418_before_body_sha256":"fbc1da1e288e6c83d48f17addafe50befe625413256c407912f51b1b444af65f","release419":{"id":393116292,"tag_name":"4.1.9","name":"4.1.9","body":"Changes made since version `4.1.8` prior to version `4.1.9`:\n\n## Other\n\n- Bump Soperator to 4.1.9\n   - PR: #3010\n\n\n\nContributors:\n@yozel\n\n| \ud83d\udcc1 **Categorized PRs** | \ud83d\udcc2 **Uncategorized PRs** | \ud83d\udce5 **Commits** | \u2795 **Lines added** | \u2796 **Lines deleted** |\n| :---: | :---: | :---: | :---: | :---: |\n| 52 | 41 | 2 | 74 | 74 |\n","target_commitish":"main","draft":false,"prerelease":false,"created_at":"2026-09-21T15:21:56Z","published_at":"2026-09-21T16:26:22Z"}}''')
REPOSITORY = "nebius/soperator"
BRANCH = "refs/heads/users/yozel/restore-4.1.8-metadata"
ACCEPT = ", ".join([
    "application/vnd.oci.image.index.v1+json",
    "application/vnd.oci.image.manifest.v1+json",
    "application/vnd.docker.distribution.manifest.list.v2+json",
    "application/vnd.docker.distribution.manifest.v2+json",
])


def require(condition, message):
    if not condition:
        raise RuntimeError(message)


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        raise RuntimeError("Unexpected HTTP redirect; credentials were not forwarded")


HTTP = urllib.request.build_opener(NoRedirect())
TOKENS = {}


def request(url, headers, method="GET", body=None):
    require(urllib.parse.urlsplit(url).hostname == "ghcr.io", "Unexpected registry host")
    try:
        with HTTP.open(urllib.request.Request(url, data=body, headers=headers, method=method), timeout=60) as response:
            return response.read(), response.headers
    except urllib.error.HTTPError as error:
        raise RuntimeError(f"GHCR {method} failed with HTTP {error.code}") from None


def token(repository, publish=False):
    key = (repository, publish)
    if key not in TOKENS:
        credentials = f"{os.environ['GITHUB_ACTOR']}:{os.environ['GH_TOKEN']}".encode()
        query = urllib.parse.urlencode({"service": "ghcr.io", "scope": f"repository:{repository}:pull" + (",push" if publish else "")})
        data, _ = request("https://ghcr.io/token?" + query, {
            "Authorization": "Basic " + base64.b64encode(credentials).decode(),
        })
        result = json.loads(data)
        TOKENS[key] = result.get("token") or result.get("access_token")
        require(TOKENS[key], "No GHCR token returned")
    return TOKENS[key]


def manifest(ref, *, digest=None, body=None, content_type=None):
    require(ref.startswith("ghcr.io/nebius/soperator/"), "Reference outside the approved namespace")
    repository, tag = ref.removeprefix("ghcr.io/").rsplit(":", 1)
    method = "PUT" if body is not None else "GET"
    headers = {"Authorization": "Bearer " + token(repository, method == "PUT"), "Accept": ACCEPT}
    if content_type:
        headers["Content-Type"] = content_type
    data, response_headers = request(f"https://ghcr.io/v2/{repository}/manifests/{digest or tag}", headers, method, body)
    if method == "GET":
        actual = "sha256:" + hashlib.sha256(data).hexdigest()
        require(response_headers.get("Docker-Content-Digest") == actual, f"Digest header mismatch: {ref}")
        return actual, data, response_headers.get_content_type()
    return response_headers.get("Docker-Content-Digest")


def api(path, payload=None):
    args = ["gh", "api", f"repos/{REPOSITORY}/{path}"]
    if payload is not None:
        args += ["--method", "PATCH", "--input", "-"]
    result = subprocess.run(args, input=json.dumps(payload) if payload is not None else None,
                            text=True, capture_output=True, check=False)
    require(result.returncode == 0, f"GitHub API failed: {path}; status {result.returncode}")
    return json.loads(result.stdout)


def ref_sha(ref):
    result = api("git/ref/" + ref.removeprefix("refs/"))
    require(result["object"]["type"] == "commit", f"Expected a direct commit reference: {ref}")
    return result["object"]["sha"]


def protected_state():
    for ref, expected in CONFIG["protected_git_refs"].items():
        require(ref_sha(ref) == expected, f"Protected Git ref changed: {ref}")
    release = api("releases/tags/4.1.9")
    require({k: release[k] for k in CONFIG["release419"]} == CONFIG["release419"], "4.1.9 release metadata changed")
    latest = api("releases/latest")
    require(latest["tag_name"] == "4.1.9", "Latest release is no longer 4.1.9")
    for entry in CONFIG["protected"]:
        require(manifest(entry["ref"])[0] == entry["digest"], f"Protected GHCR digest changed: {entry['ref']}")
    return {"latest_id": latest["id"], "latest_tag": latest["tag_name"]}


def preflight_metadata():
    current = ref_sha("refs/tags/4.1.8")
    require(current in [CONFIG["git_tag"]["expected_current_sha"], CONFIG["git_tag"]["restore_sha"]], "Unexpected 4.1.8 Git tag")
    release = api("releases/tags/4.1.8")
    require(release["id"] == CONFIG["release418_id"], "4.1.8 release ID changed")
    body_sha = hashlib.sha256(release["body"].encode()).hexdigest()
    require(body_sha == CONFIG["release418_before_body_sha256"] or release["body"] == CONFIG["release_patch"]["body"], "4.1.8 release notes changed since review")


def restore():
    require(os.environ.get("GITHUB_ACTIONS") == "true", "Execution is restricted to the approved workflow")
    require(os.environ.get("GITHUB_REPOSITORY") == REPOSITORY, "Unexpected repository")
    require(os.environ.get("GITHUB_REF") == BRANCH, "Unexpected workflow branch")
    require(os.environ.get("ROLLBACK_CONFIRMATION") == "restore-4.1.8", "Explicit dispatch confirmation missing")
    require(len(CONFIG["actions"]) == 16 and len(CONFIG["protected"]) == 17, "Unexpected approved inventory size")
    preflight_metadata()
    before = protected_state()
    originals = {}
    for entry in CONFIG["actions"]:
        ref = entry["ref"]
        require(ref.rsplit(":", 1)[1].startswith("4.1.8"), "Non-4.1.8 write target")
        require(manifest(ref)[0] in [entry["expected_current_digest"], entry["restore_digest"]], f"Unexpected current digest: {ref}")
        digest, body, media_type = manifest(ref, digest=entry["restore_digest"])
        require(digest == entry["restore_digest"] and media_type == entry["content_type"], f"Original manifest verification failed: {ref}")
        originals[ref] = body
        token(ref.removeprefix("ghcr.io/").rsplit(":", 1)[0], publish=True)
    print("All 16 original manifests, current tags, and 17 protected GHCR references passed preflight.")
    try:
        for entry in CONFIG["actions"]:
            ref = entry["ref"]
            current = manifest(ref)[0]
            if current == entry["restore_digest"]:
                print(f"Already restored: {ref}")
                continue
            require(current == entry["expected_current_digest"], f"Concurrent tag change: {ref}")
            pushed = manifest(ref, body=originals[ref], content_type=entry["content_type"])
            require(pushed == entry["restore_digest"] and manifest(ref)[0] == pushed, f"Restore verification failed: {ref}")
            print(f"Restored: {ref} -> {pushed}")
        require(protected_state() == before, "Protected state changed during GHCR restoration")
        preflight_metadata()
        current = ref_sha("refs/tags/4.1.8")
        if current != CONFIG["git_tag"]["restore_sha"]:
            subprocess.run(["git", "push", f"--force-with-lease=refs/tags/4.1.8:{current}", "origin",
                            CONFIG["git_tag"]["restore_sha"] + ":refs/tags/4.1.8"], check=True)
        require(ref_sha("refs/tags/4.1.8") == CONFIG["git_tag"]["restore_sha"], "Git tag restoration failed")
        updated = api(f"releases/{CONFIG['release418_id']}", CONFIG["release_patch"])
        require(updated["id"] == CONFIG["release418_id"] and updated["body"] == CONFIG["release_patch"]["body"], "Release notes restoration failed")
    finally:
        require(protected_state() == before, "Protected state changed; stop and investigate")
    print("Restored 16 GHCR tags and 4.1.8 Git/release metadata. 4.1.9 and shared NFS invariants passed.")


if __name__ == "__main__":
    try:
        restore()
    except Exception as error:
        print(f"STOP: {error}. A failed run can be partial; inspect completed steps before retrying.", file=sys.stderr)
        sys.exit(1)
