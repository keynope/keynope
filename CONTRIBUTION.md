# Contributing to Keynope

Thanks for helping make Keynope better. Contributions do not have to begin with code: an idea, a presentation anecdote, thoughtful feedback, or a description of an awkward workflow can be just as valuable.

## Join the Discussion

[Keynope Discussions](https://github.com/keynope/keynope/discussions) is the best place to:

- Propose and develop ideas.
- Suggest features or improvements.
- Share feedback and Keynope anecdotes.
- Show what you are creating.
- Ask questions and exchange presentation tips.

Ideas do not need to be fully formed. Starting a discussion early gives the community a chance to explore the problem and shape a solution together.

## Report a Bug

Use [GitHub Issues](https://github.com/keynope/keynope/issues) for concrete, reproducible bugs. Include:

- Your macOS version and processor architecture.
- Your Keynope version or commit.
- Steps that reproduce the problem.
- What you expected and what happened instead.
- Relevant screenshots, terminal output, or a minimal deck when possible.

Please use Discussions for open-ended questions, feature ideas, and feedback rather than filing them as bugs.

## Contribute Code

For substantial changes, start a Discussion before investing heavily in an implementation. This helps confirm the direction and surfaces relevant context early.

When preparing a pull request:

1. Keep the change focused and explain the problem it solves.
2. Add or update tests where the behavior can be tested automatically.
3. Update documentation when commands, controls, or user-visible behavior change.
4. Run the project checks locally.
5. Include screenshots for visual changes.

```sh
make test
make build
```

The complete build requires macOS 14 or later, Go, and Xcode Command Line Tools for the native presenter helper.

## Be Constructive

Be respectful, curious, and open-minded. Critique ideas and implementations rather than people, welcome different experience levels, and help keep Keynope a friendly place to experiment.
