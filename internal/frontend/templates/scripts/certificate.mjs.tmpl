import { mkdir, readFile, writeFile, chmod } from "node:fs/promises";
import { execFileSync } from "node:child_process";
import { X509Certificate } from "node:crypto";

const directory = new URL("../.cert/", import.meta.url);
await mkdir(directory, { recursive: true, mode: 0o700 });
let valid = false;
try {
  const cert = new X509Certificate(
    await readFile(new URL("cert.pem", directory)),
  );
  valid = Date.parse(cert.validTo) > Date.now() + 86_400_000;
} catch {
  /* A missing or expired certificate is regenerated locally. */
}
if (!valid) {
  await writeFile(
    new URL("openssl.cnf", directory),
    `
[req]
distinguished_name = dn
x509_extensions = ext
prompt = no
[dn]
CN = Pluto local development
[ext]
subjectAltName = IP:127.0.0.1,DNS:localhost
keyUsage = critical,digitalSignature,keyEncipherment
extendedKeyUsage = serverAuth
basicConstraints = critical,CA:FALSE
`,
  );
  execFileSync(
    "openssl",
    [
      "req",
      "-x509",
      "-newkey",
      "rsa:2048",
      "-nodes",
      "-days",
      "30",
      "-config",
      new URL("openssl.cnf", directory).pathname,
      "-keyout",
      new URL("key.pem", directory).pathname,
      "-out",
      new URL("cert.pem", directory).pathname,
    ],
    { stdio: "ignore" },
  );
  await chmod(new URL("key.pem", directory), 0o600);
  console.log(
    "Created a local-only HTTPS certificate. Accept its browser warning for this loopback example.",
  );
}
