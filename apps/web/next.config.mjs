/** @type {import('next').NextConfig} */
const nextConfig = {
  reactStrictMode: true,
  // Browser calls /api/v1/*; Next forwards to the Hub. UFO_HUB_UPLINK is the Hub
  // origin (Docker: http://api:8080), else derived from UFO_HUB_BIND.
  async rewrites() {
    const fromBind = () => {
      const bind = process.env.UFO_HUB_BIND || ":8080";
      return `http://${bind.startsWith(":") ? `localhost${bind}` : bind}`;
    };
    const hub = process.env.UFO_HUB_UPLINK || fromBind();
    return [{ source: "/api/v1/:path*", destination: `${hub}/v1/:path*` }];
  },
};

export default nextConfig;
