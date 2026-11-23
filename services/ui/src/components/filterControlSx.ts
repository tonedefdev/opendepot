// Below sm each filter takes a full row so the stacked controls align; from sm up they keep their intrinsic width.
export const filterControlSx = (minWidth: number) => ({
  flex: { xs: "1 1 100%", sm: "0 0 auto" },
  minWidth: { xs: 0, sm: minWidth },
});

export const filterIconSx = {
  fontSize: 18,
  color: "text.secondary",
  display: { xs: "none", sm: "block" },
};
