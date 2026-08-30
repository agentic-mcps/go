# Discoverability notes

This project treats discoverability as documentation and metadata that help a
developer decide whether the tool is useful. It does not promise a Google,
GitHub, or AI-agent ranking outcome.

## Google Search

Google's Search Essentials identify helpful, reliable, people-first content as
a core practice, alongside descriptive titles and headings, crawlable links,
and content that is accessible to search systems. Meeting those practices does
not guarantee crawling, indexing, or serving in results. [Google Search
Essentials](https://developers.google.com/search/docs/essentials)

The repository should therefore lead with an accurate product description,
copy-paste setup, supported boundaries, examples, and links to authoritative
contracts. It should not create keyword-heavy pages, doorway pages, hidden
text, or repetitive generated pages. Google defines doorway abuse as creating
similar pages to rank for queries and funneling users to a less useful
destination, and its spam policy also covers scaled content made primarily to
manipulate rankings. [Google spam
policies](https://developers.google.com/search/docs/essentials/spam-policies)

## GitHub discovery

GitHub describes repository topics as a way to help people find repositories,
contributions, and solutions; topics appear on the repository page and can be
used to browse related repositories. GitHub recommends lowercase, hyphenated
topics of at most 50 characters and limits a repository to 20 topics. [GitHub:
Classifying your repository with
topics](https://docs.github.com/en/repositories/managing-your-repositorys-settings-and-features/customizing-your-repository/classifying-your-repository-with-topics)

A README is the first practical orientation surface for many repository
visitors and should explain what the project does, why it is useful, how to
start, where to get help, and who maintains it. [GitHub: About the repository
README](https://docs.github.com/en/repositories/managing-your-repositorys-settings-and-features/customizing-your-repository/about-readmes)

GitHub supports an organization profile README in the public `.github`
repository at `profile/README.md`, and organization owners can pin up to six
repositories for public users. Those are useful orientation surfaces, not
guarantees of ranking. [GitHub: Customizing your organization's
profile](https://docs.github.com/en/organizations/collaborating-with-groups-in-organizations/customizing-your-organizations-profile)

Releases should make the supported artifact, checksums, installation path, and
compatibility constraints easy to verify. This is a repository practice based
on the project's trust boundary, not a claim that GitHub ranks releases in a
particular way.

## `llms.txt`

The root `llms.txt` is a small convenience index for agents and humans. It
should link to the README, installation, MCP setup, contracts, schemas,
evidence, security, and contribution guidance, with concise descriptions. It
is not presented as an official Google, GitHub, OpenAI, or MCP ranking signal;
the project makes no ranking or citation guarantee from publishing it.

## MCP Registry

The MCP Registry is currently documented as a preview and supports specific
package types with package-specific verification. The documented types are
npm, PyPI, NuGet, Docker/OCI, and MCPB; MCPB entries point to an MCPB artifact
hosted through a GitHub or GitLab release and include package metadata such as
`fileSha256`. [MCP Registry supported package
types](https://modelcontextprotocol.io/registry/package-types)

The current project release consists of Go binaries and ordinary tarball
archives, not one of those documented registry package types. It therefore
does not ship an invented `server.json` or claim registry publication. A later
release may qualify a real MCPB or another supported package, after verifying
the packaging and ownership requirements in the Registry documentation.

## Repository decision

The useful, defensible surface is one precise README, accurate GitHub
description and topics, a maintained organization profile, stable release
artifacts, and a small linked documentation index. Content remains written for
people first and made legible to agents as a consequence of clear structure,
stable links, explicit limitations, and source-grounded evidence.
