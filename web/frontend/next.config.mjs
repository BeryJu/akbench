/** @type {import('next').NextConfig} */
const nextConfig = {
  reactStrictMode: true,
  // The UI is served as a plain Node server next to the Go backend; nothing
  // here is statically exportable because every page reads live cluster state.
  output: "standalone",
};

export default nextConfig;
