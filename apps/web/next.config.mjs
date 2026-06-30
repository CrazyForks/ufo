/** @type {import('next').NextConfig} */
const nextConfig = {
  reactStrictMode: true,
  output: "standalone",
  // Browser calls /api/v1/*; asset URLs are canonical /v1/*.
  // Next forwards both to the Hub.
  async rewrites() {
    const fromBind = () => {
      const bind = process.env.UFO_HUB_BIND || ":8080";
      return `http://${bind.startsWith(":") ? `localhost${bind}` : bind}`;
    };
    const hub = process.env.UFO_HUB_URL || fromBind();
    return [
      { source: "/api/v1/:path*", destination: `${hub}/v1/:path*` },
      { source: "/v1/:path*", destination: `${hub}/v1/:path*` },
    ];
  },
};

export default nextConfig;
